package probe

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestCreateFixtureDefaultsToNoWrites(t *testing.T) {
	f := &fixtureRPC{}
	_, err := CreateFixture(context.Background(), f, CreateOptions{Parent: t.TempDir(), ManifestPath: filepath.Join(t.TempDir(), "fixture.json")})
	if !errors.Is(err, ErrFixtureGuard) || len(f.methods) != 0 {
		t.Fatalf("created without write flag: %v %v", err, f.methods)
	}
}

func TestCreateFixturePersistsIdentityBeforeSeed(t *testing.T) {
	manifestPath := filepath.Join(t.TempDir(), "fixture.json")
	f := &fixtureRPC{}
	m, err := CreateFixture(context.Background(), f, CreateOptions{Parent: t.TempDir(), ManifestPath: manifestPath, WriteEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	stored, err := LoadManifest(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if stored.ThreadID != m.ThreadID || m.ThreadID != "fixture-thread" {
		t.Fatal("identity not persisted")
	}
	if err := m.Authorize(true, m.ThreadID, m.Workspace); err != nil {
		t.Fatal(err)
	}
	for _, method := range f.methods {
		if method == "turn/start" {
			t.Fatal("fixture create unexpectedly ran a turn")
		}
	}
	if _, err := CreateFixture(context.Background(), f, CreateOptions{Parent: t.TempDir(), ManifestPath: manifestPath, WriteEnabled: true}); err == nil {
		t.Fatal("duplicate fixture manifest allowed")
	}
}

func TestUserInputFixturePurposeIsExplicit(t *testing.T) {
	f := &fixtureRPC{}
	m, err := CreateFixture(context.Background(), f, CreateOptions{Parent: t.TempDir(), ManifestPath: filepath.Join(t.TempDir(), "input.json"), WriteEnabled: true, Purpose: "user-input"})
	if err != nil || m.Purpose != "user-input" || !strings.Contains(f.instructions, "request_user_input") {
		t.Fatalf("purpose: %+v %v", m, err)
	}
	bad := &fixtureRPC{}
	if _, err := CreateFixture(context.Background(), bad, CreateOptions{Parent: t.TempDir(), ManifestPath: filepath.Join(t.TempDir(), "bad.json"), WriteEnabled: true, Purpose: "all-tools"}); err == nil || len(bad.methods) != 0 {
		t.Fatal("unknown purpose allowed")
	}
}

func TestCommandAcceptFixtureRestrictsToHarmlessExactCommand(t *testing.T) {
	f := &fixtureRPC{allowApproval: true}
	m, err := CreateFixture(context.Background(), f, CreateOptions{Parent: t.TempDir(), ManifestPath: filepath.Join(t.TempDir(), "accept.json"), WriteEnabled: true, Purpose: "command-accept"})
	if err != nil || m.Purpose != "command-accept" || !strings.Contains(f.instructions, "/usr/bin/true") {
		t.Fatalf("fixture: %+v %v", m, err)
	}
}

func TestFileAndPermissionFixturesStayInsideIsolatedWorkspace(t *testing.T) {
	for _, purpose := range []string{"file-change", "permission-request"} {
		f := &fixtureRPC{allowApproval: true}
		m, err := CreateFixture(context.Background(), f, CreateOptions{Parent: t.TempDir(), ManifestPath: filepath.Join(t.TempDir(), purpose+".json"), WriteEnabled: true, Purpose: purpose})
		if err != nil || m.Purpose != purpose || !strings.Contains(f.instructions, "fixture") {
			t.Fatalf("%s fixture: %+v %v", purpose, m, err)
		}
	}
}

type fixtureRPC struct {
	methods       []string
	instructions  string
	allowApproval bool
}

func (f *fixtureRPC) Call(ctx context.Context, method string, params any, result any) error {
	f.methods = append(f.methods, method)
	if method == "thread/start" {
		p := params.(map[string]any)
		f.instructions, _ = p["developerInstructions"].(string)
		if p["sandbox"] != "read-only" || (p["approvalPolicy"] != "never" && !(f.allowApproval && p["approvalPolicy"] == "on-request")) {
			return errors.New("unsafe fixture settings")
		}
		b, _ := json.Marshal(map[string]any{"thread": map[string]any{"id": "fixture-thread", "cwd": p["cwd"]}})
		return json.Unmarshal(b, result)
	}
	if method == "thread/name/set" {
		return nil
	}
	return errors.New("unexpected method")
}

func TestCommandDeclineFixtureHasNoSandboxGrant(t *testing.T) {
	f := &fixtureRPC{allowApproval: true}
	m, err := CreateFixture(context.Background(), f, CreateOptions{Parent: t.TempDir(), ManifestPath: filepath.Join(t.TempDir(), "decline.json"), WriteEnabled: true, Purpose: "command-decline"})
	if err != nil || m.Purpose != "command-decline" || !strings.Contains(f.instructions, "/usr/bin/true") {
		t.Fatalf("wrong fixture: %+v %v", m, err)
	}
}

func TestCommandCancelFixtureIsIsolatedAndApprovalRequired(t *testing.T) {
	f := &fixtureRPC{allowApproval: true}
	m, err := CreateFixture(context.Background(), f, CreateOptions{Parent: t.TempDir(), ManifestPath: filepath.Join(t.TempDir(), "cancel.json"), WriteEnabled: true, Purpose: "command-cancel"})
	if err != nil || m.Purpose != "command-cancel" || !strings.Contains(f.instructions, "/usr/bin/true") {
		t.Fatalf("fixture: %+v %v", m, err)
	}
	if err := m.Authorize(true, m.ThreadID, m.Workspace); err != nil {
		t.Fatal(err)
	}
}
