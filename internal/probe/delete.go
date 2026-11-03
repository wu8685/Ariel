package probe

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/wu8685/Ariel/internal/codex/appserver"
)

func DeleteFixture(ctx context.Context, rpc appserver.RPC, m Manifest, manifestPath string, enabled bool) error {
	if err := m.Authorize(enabled, m.ThreadID, m.Workspace); err != nil {
		return err
	}
	absManifest, err := filepath.Abs(manifestPath)
	if err != nil || filepath.Dir(absManifest) == m.Workspace || absManifest == m.Workspace {
		return ErrFixtureGuard
	}
	loaded, err := LoadManifest(absManifest)
	if err != nil || loaded != m {
		return ErrFixtureGuard
	}
	if err := rpc.Call(ctx, "thread/delete", map[string]string{"threadId": m.ThreadID}, nil); err != nil {
		return err
	}
	if err := os.RemoveAll(m.Workspace); err != nil {
		return errors.New("fixture thread deleted but workspace cleanup failed")
	}
	if err := os.Remove(absManifest); err != nil && !os.IsNotExist(err) {
		return errors.New("fixture thread deleted but manifest cleanup failed")
	}
	return nil
}
