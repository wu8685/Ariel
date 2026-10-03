package probe

import (
	"context"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/wu8685/Ariel/internal/codex/appserver"
)

type CreateOptions struct {
	Parent, ManifestPath string
	WriteEnabled         bool
	Purpose              string
}

// CreateFixture only bootstraps an isolated thread. Sending a turn is a separate
// guarded operation, so a persisted manifest exists before any model execution.
func CreateFixture(ctx context.Context, rpc appserver.RPC, opts CreateOptions) (Manifest, error) {
	var m Manifest
	if !opts.WriteEnabled || !filepath.IsAbs(opts.Parent) || !filepath.IsAbs(opts.ManifestPath) {
		return m, ErrFixtureGuard
	}
	if opts.Purpose != "" && opts.Purpose != "user-input" && opts.Purpose != "command-decline" && opts.Purpose != "command-cancel" && opts.Purpose != "command-accept" && opts.Purpose != "file-change" && opts.Purpose != "permission-request" {
		return m, ErrFixtureGuard
	}
	if _, err := os.Lstat(opts.ManifestPath); !os.IsNotExist(err) {
		return m, errors.New("fixture manifest path must not already exist")
	}
	workspace, err := os.MkdirTemp(opts.Parent, "ariel-fixture-")
	if err != nil {
		return m, errors.New("cannot create isolated fixture workspace")
	}
	workspace, err = filepath.EvalSymlinks(workspace)
	if err != nil {
		return m, err
	}
	guard := rand.Text()
	if err := os.WriteFile(filepath.Join(workspace, ".ariel-fixture"), []byte(guard), 0600); err != nil {
		return m, err
	}
	var result struct {
		Thread appserver.Thread `json:"thread"`
	}
	params := map[string]any{"cwd": workspace, "ephemeral": false, "sandbox": "read-only", "approvalPolicy": "never", "developerInstructions": "This is an isolated Ariel compatibility fixture. Do not use tools, inspect files, access the network, change settings, or create other tasks. Follow the requested literal response only."}
	if opts.Purpose == "user-input" {
		params["developerInstructions"] = "This is an isolated Ariel user-input compatibility fixture. Only when explicitly requested, use request_user_input for the specified questions. Never use any other tool, inspect files, access the network, change settings, or create other tasks. After receiving answers, follow the requested literal response. For the seed turn, do not use any tools."
	}
	if opts.Purpose == "command-decline" {
		params["approvalPolicy"] = "on-request"
		params["developerInstructions"] = "This is an isolated Ariel command-decline compatibility fixture. When explicitly requested, ask for a one-time exec_command approval for exactly /usr/bin/true with no prefix rule, no file access, and no network access. If declined, do not retry, request alternative permissions, or run anything else. Never change settings or create tasks. For the seed turn, do not use any tools."
	}
	if opts.Purpose == "command-cancel" {
		params["approvalPolicy"] = "on-request"
		params["developerInstructions"] = "This is an isolated Ariel command-cancel compatibility fixture. When explicitly requested, ask once for approval to run exactly /usr/bin/true; do not request any other command or grants. Never inspect files, access the network, change settings, or create tasks. For the seed turn, do not use tools. If the approval is cancelled, stop immediately."
	}
	if opts.Purpose == "command-accept" {
		params["approvalPolicy"] = "on-request"
		params["developerInstructions"] = "This is an isolated Ariel command-accept compatibility fixture. When explicitly requested, ask once for approval to run exactly /usr/bin/true with no prefix rule, no file access, and no network access. Never request any other command or grant, inspect files, change settings, or create tasks. For the seed turn, do not use tools."
	}
	if opts.Purpose == "file-change" {
		params["approvalPolicy"] = "on-request"
		params["developerInstructions"] = "This is an isolated Ariel file-change fixture. Only when explicitly requested, use apply_patch once to create fixture-note.txt in this fixture workspace with the exact content approved. Do not modify any other file, request any other permission, run commands, access the network, or create tasks. For the seed turn, do not use tools."
	}
	if opts.Purpose == "permission-request" {
		params["approvalPolicy"] = "on-request"
		params["developerInstructions"] = "This is an isolated Ariel permission-request fixture. Only when explicitly requested, ask for the smallest single-turn filesystem write permission for this fixture workspace; do not perform the write, run commands, read unrelated files, request network access, or create tasks. For the seed turn, do not use tools."
	}
	if err := rpc.Call(ctx, "thread/start", params, &result); err != nil {
		return m, err
	}
	m = Manifest{SchemaVersion: 1, ThreadID: result.Thread.ID, Workspace: workspace, CreatedAt: time.Now().UTC(), CreatedByProbe: true, GuardID: guard}
	m.Purpose = opts.Purpose
	if err := m.Authorize(true, result.Thread.ID, result.Thread.CWD); err != nil {
		return m, err
	}
	if err := SaveManifest(opts.ManifestPath, m); err != nil {
		return m, err
	}
	if err := rpc.Call(ctx, "thread/name/set", map[string]any{"threadId": m.ThreadID, "name": "[Ariel fixture] Desktop compatibility"}, nil); err != nil {
		return m, err
	}
	return m, nil
}
