package desktopipc

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"testing"
)

func wireFrame(body string) []byte {
	header := make([]byte, 4)
	binary.LittleEndian.PutUint32(header, uint32(len(body)))
	return append(header, body...)
}

type fragmentedReader struct{ r io.Reader }

func (r fragmentedReader) Read(p []byte) (int, error) {
	if len(p) > 1 {
		p = p[:1]
	}
	return r.r.Read(p)
}

func TestFramesHandleFragmentationAndCoalescing(t *testing.T) {
	want := []string{`{"type":"response","value":"你好"}`, `{"type":"broadcast"}`}
	var stream []byte
	for _, body := range want {
		stream = append(stream, wireFrame(body)...)
	}
	for _, fragmented := range []bool{false, true} {
		var r io.Reader = bytes.NewReader(stream)
		if fragmented {
			r = fragmentedReader{r}
		}
		for _, body := range want {
			got, err := ReadFrame(r, 1024)
			if err != nil || string(got) != body {
				t.Fatalf("got %q, %v; want %q", got, err, body)
			}
		}
		if _, err := ReadFrame(r, 1024); !errors.Is(err, io.EOF) {
			t.Fatalf("EOF: %v", err)
		}
	}
}

func TestReadFrameRejectsInvalidInput(t *testing.T) {
	oversize := make([]byte, 4)
	binary.LittleEndian.PutUint32(oversize, 0xffffffff)
	for _, tc := range []struct {
		name string
		wire []byte
		want error
	}{
		{"oversize before payload allocation", oversize, ErrFrameTooLarge},
		{"empty", wireFrame(""), ErrInvalidFrame},
		{"invalid JSON", wireFrame(`{"x":`), ErrInvalidFrame},
		{"non-object JSON", wireFrame(`null`), ErrInvalidFrame},
		{"invalid UTF-8", wireFrame("{\"x\":\"\xff\"}"), ErrInvalidFrame},
		{"partial header", []byte{1, 0}, io.ErrUnexpectedEOF},
		{"partial body", wireFrame(`{"a":1}`)[:6], io.ErrUnexpectedEOF},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ReadFrame(bytes.NewReader(tc.wire), 1024); !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}
	if _, err := ReadFrame(bytes.NewReader(wireFrame(`{}`)), 0); !errors.Is(err, ErrInvalidLimit) {
		t.Fatalf("zero limit: %v", err)
	}
}

type shortWriter struct{ bytes.Buffer }

func (w *shortWriter) Write(p []byte) (int, error) {
	if len(p) > 2 {
		p = p[:2]
	}
	return w.Buffer.Write(p)
}

func TestWriteFrameHandlesShortWrites(t *testing.T) {
	w := &shortWriter{}
	body := json.RawMessage(`{"message":"hello"}`)
	if err := WriteFrame(w, body, 1024); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(w.Bytes(), wireFrame(string(body))) {
		t.Fatalf("bad frame: %x", w.Bytes())
	}
}

func TestWriteFrameRejectsWithoutWriting(t *testing.T) {
	for _, tc := range []struct {
		body  string
		limit uint32
		want  error
	}{
		{`{"long":true}`, 2, ErrFrameTooLarge},
		{`invalid`, 1024, ErrInvalidFrame},
		{`{}`, 0, ErrInvalidLimit},
	} {
		var w bytes.Buffer
		if err := WriteFrame(&w, json.RawMessage(tc.body), tc.limit); !errors.Is(err, tc.want) {
			t.Fatalf("got %v, want %v", err, tc.want)
		}
		if w.Len() != 0 {
			t.Fatal("invalid frame partially written")
		}
	}
}
