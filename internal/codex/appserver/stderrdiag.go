package appserver

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sync"
)

const (
	appServerDiagnosticMaxBytes = 5 << 20
	appServerDiagnosticBackups  = 2
	appServerDiagnosticMaxLine  = 16 << 10
)

var (
	bearerSecret     = regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9._~+/=-]+`)
	namedSecret      = regexp.MustCompile(`(?i)(authorization|api[_ -]?key|token|secret|password)(["']?\s*[:=]\s*["']?)([^\s",}]+)`)
	diagnosticOnce   sync.Once
	diagnosticOutput io.Writer = io.Discard
)

type diagnosticWriter struct {
	mu        sync.Mutex
	dir       string
	path      string
	file      *os.File
	size      int64
	maxBytes  int64
	backups   int
	pending   []byte
	truncated bool
	closed    bool
}

func appServerDiagnosticWriter() io.Writer {
	diagnosticOnce.Do(func() {
		cacheDir, err := os.UserCacheDir()
		if err != nil {
			return
		}
		writer, err := newDiagnosticWriter(filepath.Join(cacheDir, "Ariel", "diagnostics"), appServerDiagnosticMaxBytes, appServerDiagnosticBackups)
		if err == nil {
			diagnosticOutput = writer
		}
	})
	return diagnosticOutput
}

func newDiagnosticWriter(dir string, maxBytes int64, backups int) (*diagnosticWriter, error) {
	if maxBytes <= 0 {
		maxBytes = appServerDiagnosticMaxBytes
	}
	if backups < 0 {
		backups = 0
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return nil, err
	}
	w := &diagnosticWriter{dir: dir, path: filepath.Join(dir, "app-server-stderr.log"), maxBytes: maxBytes, backups: backups}
	if err := w.open(); err != nil {
		return nil, err
	}
	return w, nil
}

func (w *diagnosticWriter) open() error {
	file, err := os.OpenFile(w.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	if err := file.Chmod(0600); err != nil {
		file.Close()
		return err
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return err
	}
	w.file, w.size = file, info.Size()
	return nil
}

func (w *diagnosticWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return len(p), nil
	}
	original := len(p)
	for len(p) > 0 {
		newline := bytes.IndexByte(p, '\n')
		segment := p
		complete := false
		if newline >= 0 {
			segment, p, complete = p[:newline], p[newline+1:], true
		} else {
			p = nil
		}
		if !w.truncated {
			remaining := appServerDiagnosticMaxLine - len(w.pending)
			if int64(remaining) > w.maxBytes {
				remaining = int(w.maxBytes)
			}
			if remaining > 0 {
				if len(segment) > remaining {
					w.pending = append(w.pending, segment[:remaining]...)
					w.truncated = true
				} else {
					w.pending = append(w.pending, segment...)
				}
			} else if len(segment) > 0 {
				w.truncated = true
			}
		}
		if complete {
			if err := w.flushLine(); err != nil {
				return original, nil
			}
		}
	}
	return original, nil
}

func (w *diagnosticWriter) flushLine() error {
	line := string(w.pending)
	if w.truncated {
		line += " [TRUNCATED]"
	}
	line = bearerSecret.ReplaceAllString(line, "Bearer [REDACTED]")
	line = namedSecret.ReplaceAllString(line, "$1$2[REDACTED]") + "\n"
	w.pending = w.pending[:0]
	w.truncated = false
	if int64(len(line)) > w.maxBytes {
		line = line[:w.maxBytes]
	}
	if w.size > 0 && w.size+int64(len(line)) > w.maxBytes {
		if err := w.rotate(); err != nil {
			return err
		}
	}
	n, err := io.WriteString(w.file, line)
	w.size += int64(n)
	return err
}

func (w *diagnosticWriter) rotate() error {
	if w.file != nil {
		_ = w.file.Close()
	}
	if w.backups > 0 {
		_ = os.Remove(w.path + "." + itoa(w.backups))
		for index := w.backups - 1; index >= 1; index-- {
			_ = os.Rename(w.path+"."+itoa(index), w.path+"."+itoa(index+1))
		}
		_ = os.Rename(w.path, w.path+".1")
	} else {
		_ = os.Remove(w.path)
	}
	w.size = 0
	return w.open()
}

func (w *diagnosticWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil
	}
	if len(w.pending) > 0 {
		_ = w.flushLine()
	}
	w.closed = true
	if w.file != nil {
		return w.file.Close()
	}
	return nil
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	var digits [20]byte
	index := len(digits)
	for value > 0 {
		index--
		digits[index] = byte('0' + value%10)
		value /= 10
	}
	return string(digits[index:])
}
