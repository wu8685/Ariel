package puck

import (
	"encoding/base64"
	"strings"
	"testing"
)

const tinyMessagePNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII="

func TestMessageImageValidationMatchesScreenshotBoundaries(t *testing.T) {
	valid := "data:image/png;base64," + tinyMessagePNG
	if err := validateMessageImages([]string{valid}); err != nil {
		t.Fatalf("valid png: %v", err)
	}
	for name, images := range map[string][]string{
		"mime":      {"data:image/svg+xml;base64,PHN2Zy8+"},
		"magic":     {"data:image/jpeg;base64," + tinyMessagePNG},
		"count":     {valid, valid, valid, valid},
		"base64":    {"data:image/png;base64,%%%%"},
		"zero-byte": {"data:image/png;base64,"},
	} {
		if err := validateMessageImages(images); err == nil {
			t.Fatalf("accepted %s", name)
		}
	}
	raw, err := base64.StdEncoding.DecodeString(tinyMessagePNG)
	if err != nil {
		t.Fatal(err)
	}
	large := "data:image/png;base64," + base64.StdEncoding.EncodeToString(append(raw, make([]byte, 2<<20)...))
	if err := validateMessageImages([]string{large, large, valid}); err == nil || !strings.Contains(err.Error(), "INVALID_ARGUMENT") {
		t.Fatalf("accepted aggregate larger than 4 MiB: %v", err)
	}
}
