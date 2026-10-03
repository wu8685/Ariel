package probe

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"
)

var ErrFixtureGuard = errors.New("write denied: fixture identity, explicit write flag, and isolated workspace must match")

type Manifest struct {
	SchemaVersion  int       `json:"schemaVersion"`
	ThreadID       string    `json:"threadId"`
	Workspace      string    `json:"workspace"`
	CreatedAt      time.Time `json:"createdAt"`
	CreatedByProbe bool      `json:"createdByProbe"`
	GuardID        string    `json:"guardId"`
	Purpose        string    `json:"purpose,omitempty"`
}

// Authorize is a protection against accidentally targeting a business thread,
// not a security boundary against a local user able to forge both files.
func (m Manifest) Authorize(writeEnabled bool, target, actualCWD string) error {
	if !writeEnabled || m.SchemaVersion != 1 || !m.CreatedByProbe || m.ThreadID == "" || target != m.ThreadID || m.CreatedAt.IsZero() || m.GuardID == "" {
		return ErrFixtureGuard
	}
	if !filepath.IsAbs(m.Workspace) || !filepath.IsAbs(actualCWD) {
		return ErrFixtureGuard
	}
	workspace, err := filepath.EvalSymlinks(m.Workspace)
	if err != nil || workspace == string(filepath.Separator) {
		return ErrFixtureGuard
	}
	cwd, err := filepath.EvalSymlinks(actualCWD)
	if err != nil || cwd != workspace {
		return ErrFixtureGuard
	}
	marker := filepath.Join(workspace, ".ariel-fixture")
	info, err := os.Lstat(marker)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 1024 {
		return ErrFixtureGuard
	}
	body, err := os.ReadFile(marker)
	if err != nil || string(body) != m.GuardID {
		return ErrFixtureGuard
	}
	return nil
}

func LoadManifest(path string) (Manifest, error) {
	var m Manifest
	f, err := os.Open(path)
	if err != nil {
		return m, errors.New("fixture manifest unavailable")
	}
	defer f.Close()
	body, err := io.ReadAll(io.LimitReader(f, 64*1024+1))
	if err != nil || len(body) > 64*1024 {
		return m, ErrFixtureGuard
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&m) != nil {
		return m, ErrFixtureGuard
	}
	if decoder.Decode(new(any)) != io.EOF {
		return m, ErrFixtureGuard
	}
	return m, nil
}

func SaveManifest(path string, m Manifest) error {
	body, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return errors.New("fixture manifest must use a new writable path")
	}
	_, writeErr := f.Write(append(body, '\n'))
	closeErr := f.Close()
	if writeErr != nil {
		return writeErr
	}
	return closeErr
}
