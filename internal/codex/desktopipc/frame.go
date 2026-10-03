// Package desktopipc isolates the version-specific Codex Desktop IPC transport.
package desktopipc

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"unicode/utf8"
)

var (
	ErrFrameTooLarge = errors.New("IPC frame exceeds configured limit")
	ErrInvalidFrame  = errors.New("IPC frame must contain a UTF-8 JSON object")
	ErrInvalidLimit  = errors.New("IPC frame limit must be positive")
)

const DefaultMaxFrameBytes uint32 = 8 << 20

// ReadFrame rejects lengths before allocation. Each client must bound concurrent reads.
func ReadFrame(r io.Reader, limit uint32) (json.RawMessage, error) {
	if limit == 0 {
		return nil, ErrInvalidLimit
	}
	var header [4]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return nil, err
	}
	size := binary.LittleEndian.Uint32(header[:])
	if size > limit {
		return nil, ErrFrameTooLarge
	}
	if size == 0 {
		return nil, ErrInvalidFrame
	}
	body := make([]byte, int(size))
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, err
	}
	if !validObject(body) {
		return nil, ErrInvalidFrame
	}
	return body, nil
}

// WriteFrame validates the complete body before any bytes reach the connection.
// Callers must serialize concurrent writes on the same connection.
func WriteFrame(w io.Writer, body json.RawMessage, limit uint32) error {
	if limit == 0 {
		return ErrInvalidLimit
	}
	if uint64(len(body)) > uint64(limit) {
		return ErrFrameTooLarge
	}
	if !validObject(body) {
		return ErrInvalidFrame
	}
	var header [4]byte
	binary.LittleEndian.PutUint32(header[:], uint32(len(body)))
	for _, part := range [][]byte{header[:], body} {
		for len(part) > 0 {
			n, err := w.Write(part)
			if n < 0 || n > len(part) {
				return io.ErrShortWrite
			}
			if err != nil {
				return err
			}
			if n == 0 {
				return io.ErrShortWrite
			}
			part = part[n:]
		}
	}
	return nil
}

func validObject(body []byte) bool {
	trimmed := bytes.TrimSpace(body)
	return len(trimmed) > 0 && trimmed[0] == '{' && utf8.Valid(body) && json.Valid(body)
}
