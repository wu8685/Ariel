package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// Isolation is the set of paths the harness is allowed to create or open.
// The Codex IPC socket and binary are explicit exceptions so a real local
// app-server can be used. config.toml and Ariel state directories are not.
type Isolation struct {
	Root           string `json:"root"`
	PuckDir        string `json:"puckDir"`
	DBPath         string `json:"dbPath"`
	HarnessDB      string `json:"harnessDb"`
	AgentStateDir  string `json:"agentStateDir"`
	BrainRoot      string `json:"brainRoot"`
	WorkspaceA     string `json:"workspaceA"`
	WorkspaceB     string `json:"workspaceB"`
	CodexBinary    string `json:"codexBinary,omitempty"`
	IPCSocket      string `json:"ipcSocket,omitempty"`
	CodexConfig    string `json:"codexConfig"`
	Passkey        bool   `json:"passkey"`
	ArielLocalRead bool   `json:"arielLocalRead"`
}

func layout(root, binary, socket string) Isolation {
	return Isolation{
		Root:          root,
		PuckDir:       filepath.Join(root, "puck"),
		DBPath:        filepath.Join(root, "puck", "puck.db"),
		HarnessDB:     filepath.Join(root, "harness.db"),
		AgentStateDir: filepath.Join(root, "agent-state"),
		BrainRoot:     filepath.Join(root, "brain"),
		WorkspaceA:    filepath.Join(root, "workspace-a"),
		WorkspaceB:    filepath.Join(root, "workspace-b"),
		CodexBinary:   binary,
		IPCSocket:     socket,
		CodexConfig:   "not-opened",
		Passkey:       false,
	}
}

func validateIsolation(iso Isolation) error {
	if iso.Passkey || iso.ArielLocalRead || iso.CodexConfig != "not-opened" {
		return errors.New("harness must not use Passkey, ArielLocal, or Codex config")
	}
	root, err := filepath.Abs(iso.Root)
	if err != nil {
		return err
	}
	for _, path := range []string{iso.PuckDir, iso.DBPath, iso.HarnessDB, iso.AgentStateDir, iso.BrainRoot, iso.WorkspaceA, iso.WorkspaceB} {
		if err := pathInside(root, path); err != nil {
			return err
		}
		if err := rejectForbidden(path); err != nil {
			return err
		}
	}
	for _, path := range []string{iso.CodexBinary, iso.IPCSocket} {
		if strings.TrimSpace(path) == "" {
			continue
		}
		if err := rejectForbidden(path); err != nil {
			return err
		}
	}
	return nil
}

func pathInside(root, path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return errors.New("path escapes the temp root: " + filepath.Base(abs))
	}
	return nil
}

func rejectForbidden(path string) error {
	cleaned := filepath.Clean(path)
	slash := strings.ToLower(filepath.ToSlash(cleaned))
	base := strings.ToLower(filepath.Base(cleaned))
	if base == "config.toml" || strings.HasSuffix(slash, "/.codex/config.toml") {
		return errors.New("refusing to touch Codex config.toml")
	}
	if strings.Contains(slash, "/application support/ariellocal") || strings.HasSuffix(slash, "/ariellocal") {
		return errors.New("refusing to touch ArielLocal state")
	}
	if strings.Contains(slash, "/.ariel/") || strings.HasSuffix(slash, "/.ariel") {
		return errors.New("refusing to touch the real Ariel state directory")
	}
	return nil
}

func prepareTree(iso Isolation) error {
	if err := validateIsolation(iso); err != nil {
		return err
	}
	for _, dir := range []string{iso.PuckDir, iso.AgentStateDir, iso.BrainRoot, iso.WorkspaceA, iso.WorkspaceB} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	return nil
}
