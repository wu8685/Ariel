package desktopipc

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"unicode/utf8"
)

const lazyOmittedJSON = `"__ARIEL_LAZY_OMITTED__"`

// ReadProjectedFrame applies the versioned compatibility projection above the
// ordinary frame fast path, even when a large follower's resident limit would
// fit the unfiltered payload. The wire frame remains bounded, while omitted
// bodies are consumed without allocating memory proportional to their size.
func ReadProjectedFrame(r io.Reader, limit, wireLimit uint32) (json.RawMessage, error) {
	if limit == 0 || wireLimit < limit {
		return nil, ErrInvalidLimit
	}
	var header [4]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return nil, err
	}
	size := binary.LittleEndian.Uint32(header[:])
	if size == 0 {
		return nil, ErrInvalidFrame
	}
	if size <= limit && size <= DefaultMaxFrameBytes {
		return readFrameBody(r, size)
	}
	if size > wireLimit {
		return nil, fmt.Errorf("%w: wire bytes %d exceed %d", ErrFrameTooLarge, size, wireLimit)
	}
	p := projectedParser{r: bufio.NewReaderSize(io.LimitReader(r, int64(size)), 32<<10), remaining: int64(size), limit: int(limit)}
	if err := p.value(true, "", ""); err != nil {
		return nil, err
	}
	if err := p.whitespace(false); err != nil || p.remaining != 0 {
		return nil, ErrInvalidFrame
	}
	body := p.out.Bytes()
	if !validObject(body) {
		return nil, ErrInvalidFrame
	}
	return append(json.RawMessage(nil), body...), nil
}

type projectedParser struct {
	r         *bufio.Reader
	remaining int64
	limit     int
	out       bytes.Buffer
}

func (p *projectedParser) read() (byte, error) {
	if p.remaining <= 0 {
		return 0, io.ErrUnexpectedEOF
	}
	b, err := p.r.ReadByte()
	if err != nil {
		return 0, err
	}
	p.remaining--
	return b, nil
}

func (p *projectedParser) write(copy bool, data ...byte) error {
	if !copy {
		return nil
	}
	if p.out.Len()+len(data) > p.limit {
		return fmt.Errorf("%w: projected output exceeds %d", ErrFrameTooLarge, p.limit)
	}
	p.out.Write(data)
	return nil
}

func (p *projectedParser) whitespace(copy bool) error {
	for p.remaining > 0 {
		b, err := p.r.Peek(1)
		if err != nil {
			return err
		}
		if b[0] != ' ' && b[0] != '\n' && b[0] != '\r' && b[0] != '\t' {
			return nil
		}
		c, _ := p.read()
		if err := p.write(copy, c); err != nil {
			return err
		}
	}
	return nil
}

func (p *projectedParser) value(copy bool, key, contextType string) error {
	if err := p.whitespace(copy); err != nil {
		return err
	}
	b, err := p.read()
	if err != nil {
		return ErrInvalidFrame
	}
	switch b {
	case '{':
		return p.object(copy, contextType)
	case '[':
		return p.array(copy, contextType)
	case '"':
		if ((key == "url" && contextType == "image") || (key == "result" && contextType == "imageGeneration")) && copy {
			return p.imageString()
		}
		if err := p.write(copy, '"'); err != nil {
			return err
		}
		return p.stringTail(copy, nil)
	default:
		return p.primitive(copy, b)
	}
}

func (p *projectedParser) object(copy bool, inheritedType string) error {
	if err := p.write(copy, '{'); err != nil {
		return err
	}
	if err := p.whitespace(copy); err != nil {
		return err
	}
	peek, err := p.r.Peek(1)
	if err != nil {
		return ErrInvalidFrame
	}
	if peek[0] == '}' {
		b, _ := p.read()
		return p.write(copy, b)
	}
	objectType := inheritedType
	for {
		b, err := p.read()
		if err != nil || b != '"' {
			return ErrInvalidFrame
		}
		raw := []byte{'"'}
		if err := p.stringTailTo(&raw, 4096); err != nil {
			return err
		}
		var key string
		if json.Unmarshal(raw, &key) != nil {
			return ErrInvalidFrame
		}
		if err := p.write(copy, raw...); err != nil {
			return err
		}
		if err := p.whitespace(copy); err != nil {
			return err
		}
		colon, err := p.read()
		if err != nil || colon != ':' {
			return ErrInvalidFrame
		}
		if err := p.write(copy, colon); err != nil {
			return err
		}
		if key == "type" && copy {
			// Only recognized string discriminators influence lazy projection.
			// Unknown metadata can also contain a field named "type" (e.g. a
			// JSON schema union); preserve any valid value within the same cap.
			start := p.out.Len()
			if err := p.value(copy, key, ""); err != nil {
				return err
			}
			objectType = ""
			rawType := bytes.TrimSpace(p.out.Bytes()[start:])
			if len(rawType) <= 256 && len(rawType) > 0 && rawType[0] == '"' {
				_ = json.Unmarshal(rawType, &objectType)
			}
		} else if omit := (key == "arguments" && objectType == "mcpToolCall") || (key == "result" && objectType == "mcpToolCall") || (key == "output" && (objectType == "mcpToolCall" || objectType == "commandExecution")) || (key == "diff" && objectType == "fileChange"); omit {
			if err := p.value(false, key, objectType); err != nil {
				return err
			}
			if err := p.write(copy, []byte(lazyOmittedJSON)...); err != nil {
				return err
			}
		} else if err := p.value(copy, key, objectType); err != nil {
			return err
		}
		if err := p.whitespace(copy); err != nil {
			return err
		}
		sep, err := p.read()
		if err != nil {
			return ErrInvalidFrame
		}
		if err := p.write(copy, sep); err != nil {
			return err
		}
		if sep == '}' {
			return nil
		}
		if sep != ',' {
			return ErrInvalidFrame
		}
		if err := p.whitespace(copy); err != nil {
			return err
		}
	}
}

func (p *projectedParser) array(copy bool, inheritedType string) error {
	if err := p.write(copy, '['); err != nil {
		return err
	}
	if err := p.whitespace(copy); err != nil {
		return err
	}
	peek, err := p.r.Peek(1)
	if err != nil {
		return ErrInvalidFrame
	}
	if peek[0] == ']' {
		b, _ := p.read()
		return p.write(copy, b)
	}
	for {
		if err := p.value(copy, "", inheritedType); err != nil {
			return err
		}
		if err := p.whitespace(copy); err != nil {
			return err
		}
		sep, err := p.read()
		if err != nil {
			return ErrInvalidFrame
		}
		if err := p.write(copy, sep); err != nil {
			return err
		}
		if sep == ']' {
			return nil
		}
		if sep != ',' {
			return ErrInvalidFrame
		}
	}
}

func (p *projectedParser) primitive(copy bool, first byte) error {
	token := []byte{first}
	for p.remaining > 0 {
		peek, err := p.r.Peek(1)
		if err != nil {
			return err
		}
		if bytes.ContainsRune([]byte(" \n\r\t,]}"), rune(peek[0])) {
			break
		}
		if len(token) >= 64 {
			return ErrInvalidFrame
		}
		b, _ := p.read()
		token = append(token, b)
	}
	if !json.Valid(append([]byte{'['}, append(token, ']')...)) {
		return ErrInvalidFrame
	}
	return p.write(copy, token...)
}

func (p *projectedParser) imageString() error {
	prefix := []byte("data:image/")
	probe := make([]byte, 0, len(prefix))
	for len(probe) < len(prefix) {
		b, err := p.read()
		if err != nil {
			return ErrInvalidFrame
		}
		if b == '"' || b == '\\' || b < 0x20 || b >= 0x80 {
			if err := p.write(true, '"'); err != nil {
				return err
			}
			if err := p.write(true, probe...); err != nil {
				return err
			}
			return p.stringTail(true, []byte{b})
		}
		probe = append(probe, b)
	}
	if bytes.Equal(probe, prefix) {
		if err := p.stringTail(false, nil); err != nil {
			return err
		}
		return p.write(true, []byte(lazyOmittedJSON)...)
	}
	if err := p.write(true, '"'); err != nil {
		return err
	}
	if err := p.write(true, probe...); err != nil {
		return err
	}
	return p.stringTail(true, nil)
}

func (p *projectedParser) stringTail(copy bool, initial []byte) error {
	for _, b := range initial {
		if err := p.stringByte(copy, b); err != nil {
			return err
		}
		if b == '"' {
			return nil
		}
	}
	for {
		b, err := p.read()
		if err != nil {
			return ErrInvalidFrame
		}
		if err := p.stringByte(copy, b); err != nil {
			return err
		}
		if b == '"' {
			return nil
		}
		if b == '\\' {
			escaped, err := p.read()
			if err != nil {
				return ErrInvalidFrame
			}
			if !bytes.ContainsRune([]byte(`"\\/bfnrtu`), rune(escaped)) {
				return ErrInvalidFrame
			}
			if err := p.write(copy, escaped); err != nil {
				return err
			}
			if escaped == 'u' {
				for range 4 {
					h, err := p.read()
					if err != nil || !isHex(h) {
						return ErrInvalidFrame
					}
					if err := p.write(copy, h); err != nil {
						return err
					}
				}
			}
		}
	}
}

func (p *projectedParser) stringByte(copy bool, b byte) error {
	if b < 0x20 {
		return ErrInvalidFrame
	}
	if b < utf8.RuneSelf {
		return p.write(copy, b)
	}
	width := utf8Width(b)
	if width == 0 {
		return ErrInvalidFrame
	}
	seq := []byte{b}
	for len(seq) < width {
		next, err := p.read()
		if err != nil {
			return ErrInvalidFrame
		}
		seq = append(seq, next)
	}
	if !utf8.Valid(seq) {
		return ErrInvalidFrame
	}
	return p.write(copy, seq...)
}

func (p *projectedParser) stringTailTo(dst *[]byte, limit int) error {
	for {
		b, err := p.read()
		if err != nil {
			return ErrInvalidFrame
		}
		if len(*dst) >= limit {
			return ErrInvalidFrame
		}
		*dst = append(*dst, b)
		if b == '"' {
			return nil
		}
		if b < 0x20 {
			return ErrInvalidFrame
		}
		if b == '\\' {
			escaped, err := p.read()
			if err != nil || len(*dst) >= limit {
				return ErrInvalidFrame
			}
			*dst = append(*dst, escaped)
			if !bytes.ContainsRune([]byte(`"\\/bfnrtu`), rune(escaped)) {
				return ErrInvalidFrame
			}
			if escaped == 'u' {
				for range 4 {
					h, err := p.read()
					if err != nil || !isHex(h) || len(*dst) >= limit {
						return ErrInvalidFrame
					}
					*dst = append(*dst, h)
				}
			}
		}
	}
}

func utf8Width(first byte) int {
	switch {
	case first >= 0xc2 && first <= 0xdf:
		return 2
	case first >= 0xe0 && first <= 0xef:
		return 3
	case first >= 0xf0 && first <= 0xf4:
		return 4
	default:
		return 0
	}
}
func isHex(b byte) bool { return b >= '0' && b <= '9' || b >= 'a' && b <= 'f' || b >= 'A' && b <= 'F' }
