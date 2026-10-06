// Package probe contains diagnostics and fixture boundaries, not production routing.
package probe

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/wu8685/Ariel/internal/codex/desktopipc"
)

type Config struct{ AppPath, Binary, Socket string }
type Environment struct {
	OSVersion             string `json:"osVersion"`
	DesktopVersion        string `json:"desktopVersion"`
	BundledCodexVersion   string `json:"bundledCodexVersion"`
	IPCProfileStatus      string `json:"ipcProfileStatus"`
	MinimumDesktopVersion string `json:"minimumDesktopVersion"`
	MinimumCodexVersion   string `json:"minimumCodexVersion"`
	PathCodexVersion      string `json:"pathCodexVersion,omitempty"`
	SelectedMatchesBundle bool   `json:"selectedMatchesBundle"`
	SocketPresent         bool   `json:"socketPresent"`
	IPCInitialized        bool   `json:"ipcInitialized"`
}

const (
	MinimumDesktopVersion = "26.930.31730"
	MinimumCodexVersion   = "0.160.0"
)

type IPCProfileStatus string

const (
	IPCProfileUnsupported IPCProfileStatus = "unsupported"
	IPCProfileVerified    IPCProfileStatus = "verified"
	IPCProfileUnverified  IPCProfileStatus = "unverified"
)

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

type parsedVersion struct {
	core [3]uint64
	pre  []string
}

func parseVersion(value string) (parsedVersion, bool) {
	var parsed parsedVersion
	if value == "" || strings.TrimSpace(value) != value {
		return parsed, false
	}
	value = strings.TrimPrefix(value, "v")
	if value == "" {
		return parsed, false
	}
	if coreAndPre, build, found := strings.Cut(value, "+"); found {
		if !validIdentifiers(build) {
			return parsed, false
		}
		value = coreAndPre
	}
	if core, pre, found := strings.Cut(value, "-"); found {
		if !validIdentifiers(pre) {
			return parsed, false
		}
		parsed.pre = strings.Split(pre, ".")
		value = core
	}
	parts := strings.Split(value, ".")
	if len(parts) != len(parsed.core) {
		return parsed, false
	}
	for i, part := range parts {
		if part == "" || !allDigits(part) {
			return parsedVersion{}, false
		}
		n, err := strconv.ParseUint(part, 10, 64)
		if err != nil {
			return parsedVersion{}, false
		}
		parsed.core[i] = n
	}
	return parsed, true
}

func allDigits(value string) bool {
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return value != ""
}

func validIdentifiers(value string) bool {
	if value == "" {
		return false
	}
	for _, identifier := range strings.Split(value, ".") {
		if identifier == "" {
			return false
		}
		for _, r := range identifier {
			if (r < '0' || r > '9') && (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') && r != '-' {
				return false
			}
		}
	}
	return true
}

func compareVersion(left, right parsedVersion) int {
	for i := range left.core {
		if left.core[i] < right.core[i] {
			return -1
		}
		if left.core[i] > right.core[i] {
			return 1
		}
	}
	if len(left.pre) == 0 && len(right.pre) == 0 {
		return 0
	}
	if len(left.pre) == 0 {
		return 1
	}
	if len(right.pre) == 0 {
		return -1
	}
	for i := 0; i < len(left.pre) && i < len(right.pre); i++ {
		leftNumber, leftNumeric := numericIdentifier(left.pre[i])
		rightNumber, rightNumeric := numericIdentifier(right.pre[i])
		if leftNumeric && rightNumeric {
			if leftNumber < rightNumber {
				return -1
			}
			if leftNumber > rightNumber {
				return 1
			}
			continue
		}
		switch {
		case leftNumeric:
			return -1
		case rightNumeric:
			return 1
		case left.pre[i] < right.pre[i]:
			return -1
		case left.pre[i] > right.pre[i]:
			return 1
		}
	}
	if len(left.pre) < len(right.pre) {
		return -1
	}
	if len(left.pre) > len(right.pre) {
		return 1
	}
	return 0
}

func numericIdentifier(value string) (uint64, bool) {
	if !allDigits(value) {
		return 0, false
	}
	n, err := strconv.ParseUint(value, 10, 64)
	return n, err == nil
}

func CheckIPCProfile(desktop, cli string) (IPCProfileStatus, error) {
	desktopVersion, desktopOK := parseVersion(desktop)
	cliVersion, cliOK := parseVersion(cli)
	minimumDesktop, _ := parseVersion(MinimumDesktopVersion)
	minimumCLI, _ := parseVersion(MinimumCodexVersion)
	if !desktopOK || !cliOK || compareVersion(desktopVersion, minimumDesktop) < 0 || compareVersion(cliVersion, minimumCLI) < 0 {
		return IPCProfileUnsupported, fmt.Errorf("Desktop %q / Codex %q is below or outside the supported version range; minimum Desktop %s / Codex %s", desktop, cli, MinimumDesktopVersion, MinimumCodexVersion)
	}
	if compareVersion(desktopVersion, minimumDesktop) == 0 && compareVersion(cliVersion, minimumCLI) == 0 {
		return IPCProfileVerified, nil
	}
	return IPCProfileUnverified, nil
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
	env := Environment{MinimumDesktopVersion: MinimumDesktopVersion, MinimumCodexVersion: MinimumCodexVersion, IPCProfileStatus: string(IPCProfileUnsupported)}
	var err error
	env.OSVersion, _ = command(ctx, "/usr/bin/sw_vers", "-productVersion")
	env.DesktopVersion, _ = DesktopVersion(ctx, cfg.AppPath)
	_, env.BundledCodexVersion, err = SelectBinary(cfg.Binary, BundledBinary(cfg.AppPath), func(p string) (string, error) { return BinaryVersion(ctx, p) })
	if err != nil {
		return env, err
	}
	status, _ := CheckIPCProfile(env.DesktopVersion, env.BundledCodexVersion)
	env.IPCProfileStatus = string(status)
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
