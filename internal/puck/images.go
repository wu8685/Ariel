package puck

import (
	"bytes"
	"encoding/base64"
	"errors"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"strings"
)

const (
	maxMessageImageBytes  = 4 << 20
	maxMessageImagePixels = 25_000_000
	maxMessageImageCount  = 3
)

// validateMessageImages intentionally mirrors Desktop Agent screenshot input:
// strict PNG/JPEG data URIs, MIME/magic agreement, pixel bounds and one shared
// decoded-byte budget across the message.
func validateMessageImages(images []string) error {
	if len(images) > maxMessageImageCount {
		return errors.New("INVALID_ARGUMENT")
	}
	total := 0
	for _, value := range images {
		mime := ""
		switch {
		case strings.HasPrefix(value, "data:image/png;base64,"):
			mime = "image/png"
		case strings.HasPrefix(value, "data:image/jpeg;base64,"):
			mime = "image/jpeg"
		default:
			return errors.New("INVALID_ARGUMENT")
		}
		raw := value[len("data:")+len(mime)+len(";base64,"):]
		if len(raw) > base64.StdEncoding.EncodedLen(maxMessageImageBytes) {
			return errors.New("INVALID_ARGUMENT")
		}
		data, err := base64.StdEncoding.Strict().DecodeString(raw)
		if err != nil || len(data) == 0 || len(data) > maxMessageImageBytes {
			return errors.New("INVALID_ARGUMENT")
		}
		config, format, err := image.DecodeConfig(bytes.NewReader(data))
		if err != nil || config.Width < 1 || config.Height < 1 || int64(config.Width) > maxMessageImagePixels || int64(config.Height) > maxMessageImagePixels || int64(config.Width)*int64(config.Height) > maxMessageImagePixels {
			return errors.New("INVALID_ARGUMENT")
		}
		if (format == "png" && mime != "image/png") || (format == "jpeg" && mime != "image/jpeg") || (format != "png" && format != "jpeg") {
			return errors.New("INVALID_ARGUMENT")
		}
		total += len(data)
		if total > maxMessageImageBytes {
			return errors.New("INVALID_ARGUMENT")
		}
	}
	return nil
}
