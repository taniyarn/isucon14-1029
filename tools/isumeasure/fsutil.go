package main

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"syscall"
)

func openFile(path string) (*os.File, error) { return os.Open(path) }

func inodeOf(f *os.File) uint64 {
	st, err := f.Stat()
	if err != nil {
		return 0
	}
	return inodeOfInfo(st)
}

func inodeOfInfo(st os.FileInfo) uint64 {
	if s, ok := st.Sys().(*syscall.Stat_t); ok {
		return uint64(s.Ino)
	}
	return 0
}

func fileSize(path string) int64 {
	st, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return st.Size()
}

func writeFileFromReader(path string, r io.Reader) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	if _, err := io.Copy(w, r); err != nil {
		return err
	}
	return w.Flush()
}

// jsonlWriter appends one JSON object per line.
type jsonlWriter struct {
	f *os.File
	w *bufio.Writer
}

func newJSONL(path string) (*jsonlWriter, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, err
	}
	return &jsonlWriter{f: f, w: bufio.NewWriter(f)}, nil
}

func (j *jsonlWriter) Write(v any) {
	if j == nil {
		return
	}
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	j.w.Write(b)
	j.w.WriteByte('\n')
}

func (j *jsonlWriter) Close() {
	if j == nil {
		return
	}
	j.w.Flush()
	j.f.Close()
}

func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0644)
}
