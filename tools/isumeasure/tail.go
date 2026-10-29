package main

import (
	"bufio"
	"compress/gzip"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// accessEvent is one parsed access-log line (ltsv preferred, combined as fallback).
type accessEvent struct {
	at      time.Time
	method  string
	uri     string
	status  int
	reqtime float64
	apptime float64
	size    int
	sess    string
	ua      string
	remote  string // $remote_addr (ltsv host:)
	offset  int64  // byte offset of the line start in the file
	raw     string
}

var combinedRe = regexp.MustCompile(`"([A-Z]+) (\S+) HTTP/[\d.]+" (\d{3})`)
var sessCookieRe = regexp.MustCompile(`(?i)([a-z0-9_\-]*(?:sess|token|auth|sid|login)[a-z0-9_\-]*)=([^;]+)`)

// sessionFromCookie picks the first session-looking cookie ("name=value") so sessions can be grouped by role (cookie name).
func sessionFromCookie(cookie string) string {
	if m := sessCookieRe.FindStringSubmatch(cookie); m != nil {
		return m[1] + "=" + m[2]
	}
	return ""
}

func parseAccessLine(line string, at time.Time) accessEvent {
	ev := accessEvent{at: at, raw: line, status: -1}
	if strings.Contains(line, "\t") && strings.Contains(line, ":") {
		for _, f := range strings.Split(line, "\t") {
			i := strings.IndexByte(f, ':')
			if i < 0 {
				continue
			}
			k, v := f[:i], f[i+1:]
			switch k {
			case "method":
				ev.method = v
			case "uri":
				ev.uri = v
			case "status":
				ev.status, _ = strconv.Atoi(v)
			case "reqtime":
				ev.reqtime, _ = strconv.ParseFloat(v, 64)
			case "apptime":
				ev.apptime, _ = strconv.ParseFloat(v, 64)
			case "size":
				ev.size, _ = strconv.Atoi(v)
			case "uid": // X-Isu-Uid response header logged by nginx (measurement patch): the true per-user key, wins over the cookie
				if v != "-" && v != "" {
					ev.sess = "uid=" + v
				}
			case "sess":
				if v != "-" && ev.sess == "" {
					ev.sess = "app_session=" + v
				}
			case "cookie":
				if v != "-" {
					ev.sess = sessionFromCookie(v)
				}
			case "ua":
				ev.ua = v
			case "host":
				ev.remote = v
			case "msec":
				if ms, err := strconv.ParseFloat(v, 64); err == nil && ms > 0 {
					ev.at = time.Unix(0, int64(ms*1e9))
				}
			}
		}
		if ev.method != "" {
			return ev
		}
	}
	if m := combinedRe.FindStringSubmatch(line); m != nil {
		ev.method, ev.uri = m[1], m[2]
		ev.status, _ = strconv.Atoi(m[3])
	}
	return ev
}

// tailer follows a file like `tail -F`: starts at the end, survives truncation and rotation.
type tailer struct {
	path   string
	mu     sync.Mutex
	offset int64
	f      *os.File
	ino    uint64
}

func newTailer(path string) *tailer { return &tailer{path: path} }

// run delivers complete lines to fn until stop is closed.
func (t *tailer) run(stop <-chan struct{}, fn func(line string, offset int64)) {
	var buf []byte
	t.reopen(true)
	for {
		select {
		case <-stop:
			return
		default:
		}
		if t.f == nil {
			time.Sleep(500 * time.Millisecond)
			t.reopen(true)
			continue
		}
		st, err := t.f.Stat()
		if err != nil || st.Size() < t.offset || t.rotated() {
			t.reopen(false)
			buf = buf[:0]
			continue
		}
		if st.Size() == t.offset {
			time.Sleep(200 * time.Millisecond)
			continue
		}
		chunk := make([]byte, 64*1024)
		n, err := t.f.ReadAt(chunk, t.offset)
		if n > 0 {
			data := append(buf, chunk[:n]...)
			lineStart := t.offset - int64(len(buf))
			for {
				i := strings.IndexByte(string(data), '\n')
				if i < 0 {
					break
				}
				fn(string(data[:i]), lineStart)
				lineStart += int64(i + 1)
				data = data[i+1:]
			}
			buf = append([]byte{}, data...)
			t.mu.Lock()
			t.offset += int64(n)
			t.mu.Unlock()
		}
		if err != nil && err != io.EOF {
			time.Sleep(200 * time.Millisecond)
		}
	}
}

func (t *tailer) reopen(seekEnd bool) {
	if t.f != nil {
		t.f.Close()
		t.f = nil
	}
	f, err := os.Open(t.path)
	if err != nil {
		return
	}
	t.f = f
	t.ino = inodeOf(f)
	t.mu.Lock()
	if seekEnd {
		st, _ := f.Stat()
		t.offset = st.Size()
	} else {
		t.offset = 0
	}
	t.mu.Unlock()
}

func (t *tailer) rotated() bool {
	st, err := os.Stat(t.path)
	if err != nil {
		return false
	}
	return inodeOfInfo(st) != t.ino
}

// Offset returns the current read position (= file size at the last read).
func (t *tailer) Offset() int64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.offset
}

// copyRange copies bytes [from, to) of path into dst (to<0 means until EOF).
func copyRange(path, dst string, from, to int64) (int64, error) {
	src, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer src.Close()
	if _, err := src.Seek(from, io.SeekStart); err != nil {
		return 0, err
	}
	var r io.Reader = src
	if to >= 0 {
		r = io.LimitReader(src, to-from)
	}
	out, err := os.Create(dst)
	if err != nil {
		return 0, err
	}
	defer out.Close()
	return io.Copy(bufio.NewWriterSize(out, 1<<20), r)
}

// copyRangeGz is copyRange with gzip output (slow logs shrink ~10x).
func copyRangeGz(path, dst string, from, to int64) (int64, error) {
	return copyRangeGzFilter(path, dst, from, to, false)
}

// copyRangeGzSQL is copyRangeGz for the slow log: string literals longer than maxSQLLiteral bytes (image blobs)
// become '…(N bytes)' on the way (sqllit.go). Returns the raw bytes read.
func copyRangeGzSQL(path, dst string, from, to int64) (int64, error) {
	return copyRangeGzFilter(path, dst, from, to, true)
}

func copyRangeGzFilter(path, dst string, from, to int64, shortenSQL bool) (int64, error) {
	src, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer src.Close()
	if _, err := src.Seek(from, io.SeekStart); err != nil {
		return 0, err
	}
	var r io.Reader = src
	if to >= 0 {
		r = io.LimitReader(src, to-from)
	}
	out, err := os.Create(dst)
	if err != nil {
		return 0, err
	}
	defer out.Close()
	gz, _ := gzip.NewWriterLevel(out, gzip.BestSpeed)
	var w io.Writer = gz
	var ls *litShortener
	if shortenSQL {
		ls = newLitShortener(gz, maxSQLLiteral)
		w = ls
	}
	n, err := io.Copy(w, r)
	if ls != nil {
		if cerr := ls.Close(); err == nil {
			err = cerr
		}
	}
	if cerr := gz.Close(); err == nil {
		err = cerr
	}
	return n, err
}

// openMaybeGz opens path, or path+".gz" transparently decompressed, whichever exists.
func openMaybeGz(path string) (io.ReadCloser, error) {
	if f, err := os.Open(path); err == nil {
		return f, nil
	}
	f, err := os.Open(path + ".gz")
	if err != nil {
		return nil, err
	}
	gz, err := gzip.NewReader(bufio.NewReaderSize(f, 1<<20)) // flate's own 4 KB buffer = one read syscall per 4 KB
	if err != nil {
		f.Close()
		return nil, err
	}
	return struct {
		io.Reader
		io.Closer
	}{gz, f}, nil
}
