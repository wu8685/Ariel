package probe

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type deleteRPC struct{ calls []string }

func (r *deleteRPC) Call(_ context.Context, method string, params any, result any) error {
	r.calls = append(r.calls, method)
	_, _ = params, result
	return nil
}

func TestDeleteFixtureRequiresGuardAndDeletesThreadBeforeFiles(t *testing.T) {
	parent := t.TempDir()
	workspace := filepath.Join(parent, "fixture")
	if err := os.Mkdir(workspace, 0700); err != nil {
		t.Fatal(err)
	}
	guard := "guard"
	if err := os.WriteFile(filepath.Join(workspace, ".ariel-fixture"), []byte(guard), 0600); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(parent, "fixture.json")
	m := Manifest{SchemaVersion: 1, ThreadID: "thread-fixture", Workspace: workspace, CreatedAt: time.Now().UTC(), CreatedByProbe: true, GuardID: guard}
	if err := SaveManifest(manifestPath, m); err != nil {
		t.Fatal(err)
	}
	rpc := &deleteRPC{}
	if err := DeleteFixture(context.Background(), rpc, m, manifestPath, true); err != nil {
		t.Fatal(err)
	}
	if len(rpc.calls) != 1 || rpc.calls[0] != "thread/delete" {
		t.Fatalf("unexpected calls: %v", rpc.calls)
	}
	if _, err := os.Stat(workspace); !os.IsNotExist(err) {
		t.Fatal("workspace still exists")
	}
	if _, err := os.Stat(manifestPath); !os.IsNotExist(err) {
		t.Fatal("manifest still exists")
	}
}
