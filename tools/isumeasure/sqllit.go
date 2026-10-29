package main

import (
	"bufio"
	"bytes"
	"io"
	"strconv"
)

// sqllit.go: long string literals (image blobs in `INSERT INTO icons … VALUES (…, _binary'<80 KB of JPEG>')`)
// made one ISUCON13 slow log 350 MB and the report 6x slower (normSQL's regexps over every byte). Both the agent
// (before it gzips the slow log slice) and the report (before normalizing) collapse any quoted literal longer than
// maxSQLLiteral bytes into '…(N bytes)'. The statement structure, the # Time / # Query_time headers and every
// number outside literals stay as they are; normSQL turns the placeholder into ? exactly like the original literal.
//
// Quoting follows normSQL's sqlStrRe (the parity that matters): '…' or "…", a backslash escapes the next byte, a
// doubled quote is two adjacent literals. One pass over the bytes, streaming, no regexp. Guard: a "\n# Time:" line
// inside a literal (unbalanced quote) ends the literal so the next slow-log entry is never swallowed.

const maxSQLLiteral = 256

var slowEntryHeader = []byte("\n# Time:")

type litShortener struct {
	w   *bufio.Writer
	max int
	q   byte   // 0: outside a literal; otherwise the opening quote
	in  int    // input bytes consumed
	lit int    // input offset of the open literal's quote
	esc bool   // previous byte inside the literal was a backslash
	buf []byte // literal content while it is still <= max bytes
	n   int    // literal content length so far
	hm  int    // bytes of slowEntryHeader matched at the end of the literal content
	err error
}

func newLitShortener(w io.Writer, max int) *litShortener {
	return &litShortener{w: bufio.NewWriterSize(w, 64<<10), max: max, buf: make([]byte, 0, max+1)}
}

func (s *litShortener) Write(p []byte) (int, error) {
	if s.err != nil {
		return 0, s.err
	}
	total := len(p)
	for len(p) > 0 {
		if s.q == 0 {
			i := bytes.IndexAny(p, `'"`)
			if i < 0 {
				s.w.Write(p)
				s.in += len(p)
				break
			}
			s.w.Write(p[:i+1])
			s.lit = s.in + i
			s.in += i + 1
			s.q, s.esc, s.n, s.hm, s.buf = p[i], false, 0, 0, s.buf[:0]
			p = p[i+1:]
			continue
		}
		i := 0
		for ; i < len(p); i++ {
			c := p[i]
			// header guard (independent of escapes: a slow-log header line is never part of a statement)
			if c == slowEntryHeader[s.hm] {
				s.hm++
				if s.hm == len(slowEntryHeader) {
					s.add(c)
					s.abort()
					i++
					break
				}
			} else if c == '\n' {
				s.hm = 1
			} else {
				s.hm = 0
			}
			if s.esc {
				s.esc = false
			} else if c == '\\' {
				s.esc = true
			} else if c == s.q {
				s.close()
				i++
				break
			}
			s.add(c)
		}
		s.in += i
		p = p[i:]
	}
	_, err := s.w.Write(nil)
	if err != nil {
		s.err = err
		return 0, err
	}
	return total, nil
}

func (s *litShortener) add(c byte) {
	s.n++
	if s.n <= s.max {
		s.buf = append(s.buf, c)
	}
}

func (s *litShortener) emitBody() {
	if s.n <= s.max {
		s.w.Write(s.buf)
		return
	}
	s.w.WriteString("…(" + strconv.Itoa(s.n) + " bytes)")
}

func (s *litShortener) close() {
	s.emitBody()
	s.w.WriteByte(s.q)
	s.q = 0
}

// abort: an unbalanced quote ran into the next slow-log entry. End the literal before the header line (closing it
// when it was collapsed, so the placeholder stays one token) and continue outside a literal.
func (s *litShortener) abort() {
	h := len(slowEntryHeader)
	s.n -= h
	if s.n <= s.max {
		s.buf = s.buf[:s.n]
		s.w.Write(s.buf)
	} else {
		s.emitBody()
		s.w.WriteByte(s.q)
	}
	s.w.Write(slowEntryHeader)
	s.q = 0
}

// Close flushes; an unterminated literal at EOF is written as it was (or collapsed, without a closing quote).
func (s *litShortener) Close() error {
	if s.q != 0 {
		s.emitBody()
		s.q = 0
	}
	if err := s.w.Flush(); err != nil {
		return err
	}
	return s.err
}

// shortenSQLLiterals is the in-memory form for one statement. Statements that cannot hold a long literal come back
// unchanged without copying.
func shortenSQLLiterals(q string) string {
	if len(q) <= maxSQLLiteral+2 {
		return q
	}
	var b bytes.Buffer
	b.Grow(len(q))
	s := newLitShortener(&b, maxSQLLiteral)
	io.WriteString(s, q)
	if s.q != 0 {
		// unterminated (processlist info is cut at 160 chars): keep that tail as it was, like sqlStrRe does
		return shortenSQLLiterals(q[:s.lit]) + q[s.lit:]
	}
	s.Close()
	if b.Len() == len(q) {
		return q
	}
	return b.String()
}

// shortenLiteralsReader streams rc through the shortener (slow logs shipped by an agent that did not shorten yet).
// Pull-based (no goroutine/pipe: a pipe hand-off per chunk cost more than the scan). Close closes rc.
func shortenLiteralsReader(rc io.ReadCloser) io.ReadCloser {
	r := &litReader{rc: rc, in: make([]byte, 256<<10)}
	r.s = newLitShortener(&r.out, maxSQLLiteral)
	return r
}

type litReader struct {
	rc  io.ReadCloser
	s   *litShortener
	in  []byte
	out bytes.Buffer
	eof bool
}

func (r *litReader) Read(p []byte) (int, error) {
	for r.out.Len() == 0 && !r.eof {
		n, err := r.rc.Read(r.in)
		if n > 0 {
			r.s.Write(r.in[:n])
			r.s.w.Flush()
		}
		if err == io.EOF {
			r.s.Close()
			r.eof = true
		} else if err != nil {
			return 0, err
		}
	}
	if r.out.Len() == 0 {
		return 0, io.EOF
	}
	return r.out.Read(p)
}

func (r *litReader) Close() error { return r.rc.Close() }
