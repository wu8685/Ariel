package probe

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFixtureGuardRequiresWriteFlagIdentityAndIsolatedWorkspace(t *testing.T) {
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, ".ariel-fixture"), []byte("guard-token"), 0600); err != nil {
		t.Fatal(err)
	}
	valid := Manifest{SchemaVersion: 1, ThreadID: "fixture-thread", Workspace: workspace, CreatedAt: time.Now().UTC(), CreatedByProbe: true, GuardID: "guard-token"}
	for _, tc := range []struct {
		name        string
		enabled     bool
		target, cwd string
		change      func(*Manifest)
		ok          bool
	}{
		{"valid", true, "fixture-thread", workspace, nil, true},
		{"read only", false, "fixture-thread", workspace, nil, false},
		{"wrong thread", true, "business-thread", workspace, nil, false},
		{"wrong cwd", true, "fixture-thread", t.TempDir(), nil, false},
		{"not created by probe", true, "fixture-thread", workspace, func(m *Manifest) { m.CreatedByProbe = false }, false},
		{"missing timestamp", true, "fixture-thread", workspace, func(m *Manifest) { m.CreatedAt = time.Time{} }, false},
		{"unknown manifest", true, "fixture-thread", workspace, func(m *Manifest) { m.SchemaVersion = 99 }, false},
		{"wrong marker", true, "fixture-thread", workspace, func(m *Manifest) { m.GuardID = "different" }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := valid
			if tc.change != nil {
				tc.change(&m)
			}
			err := m.Authorize(tc.enabled, tc.target, tc.cwd)
			if (err == nil) != tc.ok {
				t.Fatalf("authorization = %v", err)
			}
		})
	}
}

func TestFixtureManifestRoundTripDoesNotOverwrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fixture.json")
	m := Manifest{SchemaVersion: 1, ThreadID: "fixture-thread", Workspace: dir, CreatedAt: time.Now().UTC(), CreatedByProbe: true, GuardID: "marker"}
	if err := SaveManifest(path, m); err != nil {
		t.Fatal(err)
	}
	if err := SaveManifest(path, m); err == nil {
		t.Fatal("overwrote existing fixture identity")
	}
	loaded, err := LoadManifest(path)
	if err != nil || loaded.ThreadID != m.ThreadID {
		t.Fatalf("got %+v %v", loaded, err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatalf("permissions: %v", info.Mode())
	}
}
