package appserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiagnosticWriterRedactsSecretsAndRotates(t *testing.T) {
	dir := t.TempDir()
	writer, err := newDiagnosticWriter(dir, 96, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	lines := []string{
		"token=plain-token warning\n",
		`Authorization: Bearer abc.def.ghi` + "\n",
		`{"api_key":"key-value","message":"safe"}` + "\n",
		"password: hunter2\n",
		strings.Repeat("x", 200) + "\n",
	}
	for range 4 {
		for _, line := range lines {
			if _, err := writer.Write([]byte(line)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	entries, err := filepath.Glob(filepath.Join(dir, "app-server-stderr.log*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 || len(entries) > 3 {
		t.Fatalf("files = %v", entries)
	}
	for _, path := range entries {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		text := string(body)
		for _, secret := range []string{"plain-token", "abc.def.ghi", "key-value", "hunter2"} {
			if strings.Contains(text, secret) {
				t.Fatalf("%s contains secret %q", filepath.Base(path), secret)
			}
		}
		if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0600 {
			t.Fatalf("mode %s: %v %v", path, info, err)
		}
	}
}
