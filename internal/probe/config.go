// Package probe contains diagnostics and fixture boundaries, not production routing.
package probe

import (
	"context"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/wu8685/Ariel/internal/codex/desktopipc"
)

type Config struct{ AppPath, Binary, Socket string }
type Environment struct {
	OSVersion             string `json:"osVersion"`
	DesktopVersion        string `json:"desktopVersion"`
	BundledCodexVersion   string `json:"bundledCodexVersion"`
	PathCodexVersion      string `json:"pathCodexVersion,omitempty"`
	SelectedMatchesBundle bool   `json:"selectedMatchesBundle"`
	SocketPresent         bool   `json:"socketPresent"`
	IPCInitialized        bool   `json:"ipcInitialized"`
}

func Defaults() (Config, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Config{}, err
	}
	return Config{AppPath: "/Applications/ChatGPT.app", Socket: filepath.Join(home, ".codex", "ipc", "ipc.sock")}, nil
}
func BundledBinary(app string) string {
	return filepath.Join(app, "Contents", "Resources", "codex-cli", "CodexCLI.app", "Contents", "MacOS", "codex")
}

func IPCProfileVerified(desktop, cli string) bool {
	return desktop == "26.930.31730" && cli == "0.160.0"
}

func DesktopVersion(ctx context.Context, app string) (string, error) {
	return command(ctx, "/usr/libexec/PlistBuddy", "-c", "Print :CFBundleShortVersionString", filepath.Join(app, "Contents", "Info.plist"))
}

func SelectBinary(explicit, bundled string, version func(string) (string, error)) (string, string, error) {
	bundleVersion, err := version(bundled)
	if err != nil {
		return "", "", errors.New("Desktop bundled binary unavailable")
	}
	if explicit == "" {
		return bundled, bundleVersion, nil
	}
	selectedVersion, err := version(explicit)
	if err != nil {
		return "", "", errors.New("configured Codex binary unavailable")
	}
	if selectedVersion != bundleVersion {
		return "", "", errors.New("configured Codex version differs from Desktop bundle")
	}
	return explicit, selectedVersion, nil
}
func BinaryVersion(ctx context.Context, binary string) (string, error) {
	output, err := command(ctx, binary, "--version")
	if err != nil {
		return "", err
	}
	version, ok := strings.CutPrefix(output, "codex-cli ")
	if !ok || version == "" {
		return "", errors.New("unrecognized Codex binary version")
	}
	return version, nil
}
func command(ctx context.Context, binary string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	b, err := exec.CommandContext(ctx, binary, args...).Output()
	if err != nil {
		return "", errors.New("environment command failed")
	}
	return strings.TrimSpace(string(b)), nil
}
func Detect(ctx context.Context, cfg Config) (Environment, error) {
	var env Environment
	var err error
	env.OSVersion, _ = command(ctx, "/usr/bin/sw_vers", "-productVersion")
	env.DesktopVersion, _ = DesktopVersion(ctx, cfg.AppPath)
	_, env.BundledCodexVersion, err = SelectBinary(cfg.Binary, BundledBinary(cfg.AppPath), func(p string) (string, error) { return BinaryVersion(ctx, p) })
	if err != nil {
		return env, err
	}
	env.SelectedMatchesBundle = true
	if binary, err := exec.LookPath("codex"); err == nil {
		env.PathCodexVersion, _ = BinaryVersion(ctx, binary)
	}
	if stat, err := os.Stat(cfg.Socket); err == nil {
		env.SocketPresent = stat.Mode()&os.ModeSocket != 0
	}
	if !env.SocketPresent {
		return env, nil
	}
	c, err := Connect(ctx, cfg.Socket)
	if err != nil {
		return env, err
	}
	defer c.Close()
	env.IPCInitialized = true
	return env, nil
}
func Connect(ctx context.Context, socket string) (*desktopipc.Client, error) {
	return ConnectThread(ctx, socket, "")
}
func ConnectThread(ctx context.Context, socket, threadID string) (*desktopipc.Client, error) {
	d := net.Dialer{Timeout: 5 * time.Second}
	conn, err := d.DialContext(ctx, "unix", socket)
	if err != nil {
		return nil, errors.New("Desktop IPC unavailable")
	}
	c := desktopipc.NewClient(conn, desktopipc.Options{ThreadID: threadID})
	if err := c.Initialize(ctx); err != nil {
		c.Close()
		return nil, err
	}
	return c, nil
}
