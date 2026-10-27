package desktopipc

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"runtime"
	"strings"
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

func TestReadProjectedFrameStreamsOversizedToolAndImagePayloads(t *testing.T) {
	body := []byte(`{"type":"broadcast","params":{"conversationId":"thread","items":[{"id":"tool","type":"mcpToolCall","result":{"content":"` + strings.Repeat("x", 2<<20) + `"}},{"id":"image","type":"userMessage","content":[{"type":"image","url":"data:image/png;base64,` + strings.Repeat("A", 2<<20) + `"}]}]}}`)
	projected, err := ReadProjectedFrame(bytes.NewReader(wireFrame(string(body))), 8<<10, 8<<20)
	if err != nil {
		t.Fatal(err)
	}
	if len(projected) >= 8<<10 || bytes.Contains(projected, []byte(strings.Repeat("x", 128))) || bytes.Contains(projected, []byte("data:image/")) || bytes.Count(projected, []byte("__ARIEL_LAZY_OMITTED__")) != 2 {
		t.Fatalf("large payload was not projected safely: bytes=%d body=%s", len(projected), projected)
	}
}

func TestReadProjectedFrameProjectsLargeKnownPayloadBeforeResidentLimit(t *testing.T) {
	const payloadBytes = 9 << 20
	for _, kind := range []string{"tool", "image", "ordinary"} {
		t.Run(kind, func(t *testing.T) {
			payload := strings.Repeat("x", payloadBytes)
			var body string
			switch kind {
			case "tool":
				body = `{"type":"broadcast","params":{"items":[{"id":"tool","type":"mcpToolCall","result":{"type":42,"content":"` + payload + `"}}],"cwd":"/fixture","text":"keep me"}}`
			case "image":
				body = `{"type":"broadcast","params":{"items":[{"type":"image","url":"data:image/png;base64,` + payload + `"}],"text":"keep me"}}`
			default:
				body = `{"type":"broadcast","params":{"unknown":"` + payload + `","text":"keep me"}}`
			}
			projected, err := ReadProjectedFrame(bytes.NewReader(wireFrame(body)), 64<<20, 256<<20)
			if err != nil {
				t.Fatalf("valid compatible frame rejected: %v", err)
			}
			if kind == "ordinary" {
				if !bytes.Equal(projected, []byte(body)) {
					t.Fatal("unknown content was silently removed")
				}
			} else if len(projected) > 4096 || !bytes.Contains(projected, []byte(lazyOmittedJSON)) || !bytes.Contains(projected, []byte("keep me")) {
				t.Fatalf("known lazy payload bypassed projection below resident cap: bytes=%d", len(projected))
			}
		})
	}
}

func TestReadProjectedFrameFailsClosedForUnknownOversizedPayload(t *testing.T) {
	body := `{"type":"broadcast","params":{"conversationId":"thread","unknown":"` + strings.Repeat("x", 32<<10) + `"}}`
	if _, err := ReadProjectedFrame(bytes.NewReader(wireFrame(body)), 8<<10, 64<<10); !errors.Is(err, ErrFrameTooLarge) {
		t.Fatalf("unknown oversized field must fail closed: %v", err)
	}
}

func TestReadProjectedFrameDoesNotAllocateProportionalToOmittedPayload(t *testing.T) {
	body := `{"type":"broadcast","params":{"items":[{"type":"mcpToolCall","result":{"content":"` + strings.Repeat("x", 32<<20) + `"}}]}}`
	wire := wireFrame(body)
	body = ""
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	projected, err := ReadProjectedFrame(bytes.NewReader(wire), 8<<10, 64<<20)
	runtime.ReadMemStats(&after)
	if err != nil || len(projected) >= 8<<10 {
		t.Fatalf("projection failed: bytes=%d err=%v", len(projected), err)
	}
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 4<<20 {
		t.Fatalf("projection allocated with omitted payload: allocated=%d", allocated)
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
