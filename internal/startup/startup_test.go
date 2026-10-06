package startup

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

func TestParseTwoSetupModesAndRejectUnknownFlags(t *testing.T) {
	local, err := parseArgs([]string{"up", "local", "--listen", "127.0.0.1:18080", "--device-id", "test-mac", "--device-name", "Test Mac"})
	if err != nil || local.command != "up" || local.mode != "local" || local.listen != "127.0.0.1:18080" || local.deviceName != "Test Mac" {
		t.Fatalf("local: %+v, %v", local, err)
	}
	remote, err := parseArgs([]string{"up", "agent", "--relay-url", "wss://relay.example/ws", "--token-file", "/tmp/token", "--device-id", "test-mac", "--device-name", "Test Mac"})
	if err != nil || remote.mode != "agent" || remote.relayURL != "wss://relay.example/ws" {
		t.Fatalf("agent: %+v, %v", remote, err)
	}
	if _, err := parseArgs([]string{"up", "local", "--bogus"}); err == nil {
		t.Fatal("unknown flag accepted")
	}
	restart, err := parseArgs([]string{"restart-local"})
	if err != nil || restart.command != "restart-local" {
		t.Fatalf("restart-local: %+v, %v", restart, err)
	}
	if _, err := parseArgs([]string{"restart-local", "--listen", "192.168.1.2:8080"}); err == nil {
		t.Fatal("restart-local accepted manual network arguments")
	}
}

func TestConfigPersistsPrivateIndependentCredentials(t *testing.T) {
	root := t.TempDir()
	opts, _ := parseArgs([]string{"up", "local", "--listen", "127.0.0.1:18080", "--device-id", "test-mac", "--device-name", "Test Mac"})
	cfg, err := prepareConfig(root, opts)
	if err != nil || cfg.Mode != "local" || cfg.RelayURL != "ws://127.0.0.1:18080/ws" {
		t.Fatalf("prepare: %+v, %v", cfg, err)
	}
	runtime := filepath.Join(root, ".local", "runtime")
	if info, err := os.Stat(runtime); err != nil || info.Mode().Perm() != 0700 {
		t.Fatalf("runtime mode: %v, %v", info, err)
	}
	token, err := os.ReadFile(filepath.Join(runtime, "agent-token"))
	if err != nil || len(strings.TrimSpace(string(token))) < 32 {
		t.Fatalf("missing strong token: %v", err)
	}
	pin, err := os.ReadFile(filepath.Join(runtime, "web-pin"))
	if err != nil || !regexp.MustCompile(`^[0-9]{6}$`).MatchString(strings.TrimSpace(string(pin))) {
		t.Fatalf("invalid PIN: %v", err)
	}
	if strings.TrimSpace(string(token)) == strings.TrimSpace(string(pin)) {
		t.Fatal("token and PIN are identical")
	}
	for _, name := range []string{"agent-token", "web-pin", "config.json"} {
		info, err := os.Stat(filepath.Join(runtime, name))
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatalf("%s mode: %v, %v", name, info, err)
		}
	}
	configBody, _ := os.ReadFile(filepath.Join(runtime, "config.json"))
	if strings.Contains(string(configBody), string(token)) || strings.Contains(string(configBody), string(pin)) {
		t.Fatal("config leaks credentials")
	}
	loaded, err := loadConfig(root)
	if err != nil || loaded != cfg {
		t.Fatalf("loaded config: %+v, %v", loaded, err)
	}
	if _, err := prepareConfig(root, opts); err != nil {
		t.Fatalf("same config should be idempotent: %v", err)
	}
	tokenAfter, _ := os.ReadFile(filepath.Join(runtime, "agent-token"))
	pinAfter, _ := os.ReadFile(filepath.Join(runtime, "web-pin"))
	if string(tokenAfter) != string(token) || string(pinAfter) != string(pin) {
		t.Fatal("repeat setup rotated credentials")
	}
}

func TestRejectInvalidOrInsecureConfigurationBeforeSaving(t *testing.T) {
	tests := [][]string{
		{"up", "local", "--listen", "0.0.0.0:8080", "--device-id", "a", "--device-name", "A"},
		{"up", "local", "--listen", "example.test:8080", "--device-id", "a", "--device-name", "A"},
		{"up", "agent", "--relay-url", "ws://relay.example/ws", "--token-file", "/missing", "--device-id", "a", "--device-name", "A"},
		{"up", "agent", "--relay-url", "wss://relay.example/other", "--token-file", "/missing", "--device-id", "a", "--device-name", "A"},
	}
	for _, args := range tests {
		root := t.TempDir()
		opts, err := parseArgs(args)
		if err == nil {
			_, err = prepareConfig(root, opts)
		}
		if err == nil {
			t.Fatalf("accepted invalid args: %v", args)
		}
		if _, err := os.Stat(filepath.Join(root, ".local", "runtime", "config.json")); !os.IsNotExist(err) {
			t.Fatalf("invalid config was saved: %v", args)
		}
	}
}

func TestRefuseSymlinkedPrivateRuntime(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".local"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, ".local", "runtime")); err != nil {
		t.Fatal(err)
	}
	opts, _ := parseArgs([]string{"up", "local", "--listen", "127.0.0.1:18080", "--device-id", "safe", "--device-name", "Safe"})
	if _, err := prepareConfig(root, opts); err == nil {
		t.Fatal("followed symlinked secret directory")
	}
	entries, _ := os.ReadDir(outside)
	if len(entries) != 0 {
		t.Fatal("secrets were written outside repository runtime")
	}
}

func TestImportPINAndRejectImplicitConfigChange(t *testing.T) {
	root := t.TempDir()
	pinFile := filepath.Join(root, "input-pin")
	if err := os.WriteFile(pinFile, []byte("012345\n"), 0600); err != nil {
		t.Fatal(err)
	}
	opts, _ := parseArgs([]string{"up", "local", "--listen", "127.0.0.1:18081", "--device-id", "same", "--device-name", "Same", "--pin-file", pinFile})
	if _, err := prepareConfig(root, opts); err != nil {
		t.Fatal(err)
	}
	pin, _ := os.ReadFile(filepath.Join(root, ".local", "runtime", "web-pin"))
	if strings.TrimSpace(string(pin)) != "012345" {
		t.Fatalf("imported PIN changed")
	}
	changed := opts
	changed.listen = "127.0.0.1:18082"
	changed.pinFile = ""
	if _, err := prepareConfig(root, changed); err == nil {
		t.Fatal("implicit reconfiguration accepted")
	}
}

func TestUpdateLocalListenPreservesCredentialsAndDeviceIdentity(t *testing.T) {
	root := t.TempDir()
	opts, _ := parseArgs([]string{"up", "local", "--listen", "192.168.1.2:18081", "--device-id", "same-device", "--device-name", "Same Device"})
	before, err := prepareConfig(root, opts)
	if err != nil {
		t.Fatal(err)
	}
	tokenBefore, _ := os.ReadFile(filepath.Join(runtimeDir(root), "agent-token"))
	pinBefore, _ := os.ReadFile(filepath.Join(runtimeDir(root), "web-pin"))
	after, err := updateLocalListen(root, before, "10.1.2.3:18081")
	if err != nil {
		t.Fatal(err)
	}
	if after.Listen != "10.1.2.3:18081" || after.RelayURL != "ws://10.1.2.3:18081/ws" || after.DeviceID != before.DeviceID || after.DeviceName != before.DeviceName {
		t.Fatalf("unexpected updated config: %+v", after)
	}
	tokenAfter, _ := os.ReadFile(filepath.Join(runtimeDir(root), "agent-token"))
	pinAfter, _ := os.ReadFile(filepath.Join(runtimeDir(root), "web-pin"))
	if string(tokenAfter) != string(tokenBefore) || string(pinAfter) != string(pinBefore) {
		t.Fatal("network update changed credential bytes")
	}
	loaded, err := loadConfig(root)
	if err != nil || loaded != after {
		t.Fatalf("saved update: %+v, %v", loaded, err)
	}
}

func TestRestartLocalPreflightsBeforeStoppingAndPreservesConfigOnFailure(t *testing.T) {
	root := t.TempDir()
	opts, _ := parseArgs([]string{"up", "local", "--listen", "192.168.1.2:18082", "--device-id", "mac", "--device-name", "Mac"})
	before, err := prepareConfig(root, opts)
	if err != nil {
		t.Fatal(err)
	}
	stopped, built := false, false
	ops := restartLocalOps{
		discover:     func(context.Context) (string, error) { return "192.168.2.3", nil },
		managedState: func(string) (bool, bool, error) { return true, true, nil },
		checkPort:    func(string) error { return errors.New("occupied") },
		build:        func(context.Context, string, config, io.Writer) error { built = true; return nil },
		stop:         func(string, io.Writer) error { stopped = true; return nil },
	}
	if err := restartLocalWith(context.Background(), root, io.Discard, ops); err == nil || !strings.Contains(err.Error(), "occupied") {
		t.Fatalf("foreign port was not rejected: %v", err)
	}
	if stopped || built {
		t.Fatalf("preflight failure stopped=%v built=%v", stopped, built)
	}
	after, err := loadConfig(root)
	if err != nil || after != before {
		t.Fatalf("preflight failure changed config: %+v, %v", after, err)
	}
}

func TestRestartLocalUpdatesAfterBuildAndHandlesPartialManagedState(t *testing.T) {
	root := t.TempDir()
	opts, _ := parseArgs([]string{"up", "local", "--listen", "192.168.1.2:18083", "--device-id", "mac", "--device-name", "Mac"})
	if _, err := prepareConfig(root, opts); err != nil {
		t.Fatal(err)
	}
	tokenBefore, _ := os.ReadFile(filepath.Join(runtimeDir(root), "agent-token"))
	pinBefore, _ := os.ReadFile(filepath.Join(runtimeDir(root), "web-pin"))
	events := []string{}
	ops := restartLocalOps{
		discover: func(context.Context) (string, error) {
			events = append(events, "discover")
			return "10.2.3.4", nil
		},
		managedState: func(string) (bool, bool, error) {
			events = append(events, "managed")
			return true, false, nil
		},
		checkPort: func(listen string) error {
			events = append(events, "port:"+listen)
			return nil
		},
		build: func(context.Context, string, config, io.Writer) error {
			events = append(events, "build")
			return nil
		},
		stop: func(string, io.Writer) error {
			events = append(events, "stop")
			return nil
		},
		start: func(_ string, cfg config, _ io.Writer) error {
			events = append(events, "start:"+cfg.Listen)
			loaded, err := loadConfig(root)
			if err != nil || loaded != cfg {
				return errors.New("start did not receive saved config")
			}
			return nil
		},
		status: func(string, io.Writer) error {
			events = append(events, "status")
			return nil
		},
	}
	if err := restartLocalWith(context.Background(), root, io.Discard, ops); err != nil {
		t.Fatal(err)
	}
	want := "discover,managed,port:10.2.3.4:18083,build,stop,start:10.2.3.4:18083,status"
	if got := strings.Join(events, ","); got != want {
		t.Fatalf("restart order: %s", got)
	}
	tokenAfter, _ := os.ReadFile(filepath.Join(runtimeDir(root), "agent-token"))
	pinAfter, _ := os.ReadFile(filepath.Join(runtimeDir(root), "web-pin"))
	if string(tokenAfter) != string(tokenBefore) || string(pinAfter) != string(pinBefore) {
		t.Fatal("restart changed credentials")
	}
}

func TestRestartLocalRejectsMissingAndRemoteConfiguration(t *testing.T) {
	called := false
	ops := restartLocalOps{discover: func(context.Context) (string, error) { called = true; return "192.168.1.2", nil }}
	if err := restartLocalWith(context.Background(), t.TempDir(), io.Discard, ops); err == nil {
		t.Fatal("missing config accepted")
	}
	root := t.TempDir()
	tokenFile := filepath.Join(root, "token")
	if err := os.WriteFile(tokenFile, []byte(strings.Repeat("a", 64)), 0600); err != nil {
		t.Fatal(err)
	}
	remote, _ := parseArgs([]string{"up", "agent", "--relay-url", "wss://relay.example/ws", "--device-id", "mac", "--device-name", "Mac", "--token-file", tokenFile})
	if _, err := prepareConfig(root, remote); err != nil {
		t.Fatal(err)
	}
	if err := restartLocalWith(context.Background(), root, io.Discard, ops); err == nil || !strings.Contains(err.Error(), "local mode") {
		t.Fatalf("remote config accepted: %v", err)
	}
	if called {
		t.Fatal("invalid config reached network discovery")
	}
}

func TestChangingCredentialNeedsExplicitReplace(t *testing.T) {
	root := t.TempDir()
	opts, _ := parseArgs([]string{"up", "local", "--listen", "127.0.0.1:18083", "--device-id", "same", "--device-name", "Same"})
	if _, err := prepareConfig(root, opts); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "new-token")
	if err := os.WriteFile(source, []byte(strings.Repeat("a", 64)), 0600); err != nil {
		t.Fatal(err)
	}
	opts.tokenFile = source
	if _, err := prepareConfig(root, opts); err == nil {
		t.Fatal("same config silently ignored a new credential")
	}
	opts.replace = true
	if _, err := prepareConfig(root, opts); err != nil {
		t.Fatalf("explicit replacement failed: %v", err)
	}
	token, _ := os.ReadFile(filepath.Join(runtimeDir(root), "agent-token"))
	if strings.TrimSpace(string(token)) != strings.Repeat("a", 64) {
		t.Fatal("explicit replacement did not import token")
	}
}

func TestSwitchingFromRemoteAgentToLocalRelayGeneratesIndependentToken(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "remote-token")
	remoteToken := strings.Repeat("r", 64)
	if err := os.WriteFile(source, []byte(remoteToken), 0600); err != nil {
		t.Fatal(err)
	}
	remote, _ := parseArgs([]string{"up", "agent", "--relay-url", "wss://relay.example/ws", "--device-id", "mac", "--device-name", "Mac", "--token-file", source})
	if _, err := prepareConfig(root, remote); err != nil {
		t.Fatal(err)
	}
	local, _ := parseArgs([]string{"up", "local", "--listen", "127.0.0.1:18084", "--device-id", "mac", "--device-name", "Mac", "--replace-config"})
	if _, err := prepareConfig(root, local); err != nil {
		t.Fatal(err)
	}
	localToken, _ := os.ReadFile(filepath.Join(runtimeDir(root), "agent-token"))
	if strings.TrimSpace(string(localToken)) == remoteToken {
		t.Fatal("new local Relay reused external Relay credential")
	}
}

func TestBuildPlanAndProxyIsolation(t *testing.T) {
	local := config{Mode: "local"}
	remote := config{Mode: "agent"}
	if got := strings.Join(buildSteps(local), ","); got != "npm ci,npm run build,go build relay,go build desktop-agent" {
		t.Fatalf("local build plan: %s", got)
	}
	if got := strings.Join(buildSteps(remote), ","); got != "go build desktop-agent" {
		t.Fatalf("agent build plan: %s", got)
	}
	env := localAgentEnvironment([]string{"HTTP_PROXY=http://proxy.test:7897", "NO_PROXY=localhost"}, "192.168.1.2")
	joined := strings.Join(env, "\n")
	if !strings.Contains(joined, "HTTP_PROXY=http://proxy.test:7897") || !strings.Contains(joined, "NO_PROXY=localhost,192.168.1.2") {
		t.Fatalf("LAN proxy bypass missing: %s", joined)
	}
}

func TestBuildAndChildEnvironmentDoNotInheritUnrelatedArielSecrets(t *testing.T) {
	base := []string{"PATH=/usr/bin", "ARIEL_TOKEN=old-secret", "ARIEL_WEB_PIN=012345", "ARIEL_RELAY_URL=ws://old/ws", "HTTP_PROXY=http://proxy.test"}
	clean := cleanArielEnvironment(base)
	joined := strings.Join(clean, "\n")
	if strings.Contains(joined, "ARIEL_") || !strings.Contains(joined, "PATH=/usr/bin") || !strings.Contains(joined, "HTTP_PROXY=http://proxy.test") {
		t.Fatalf("Ariel secrets leaked into build or child baseline: %s", joined)
	}
}

func TestManagedProcessIdentityMustMatchExactExecutable(t *testing.T) {
	path := "/tmp/ariel/bin/ariel-relay"
	if !commandMatches(path, path) || commandMatches(path, path+"-other") || commandMatches(path, "/bin/sh "+path) {
		t.Fatal("unsafe process command comparison")
	}
	record := managedProcess{PID: 123, Executable: path, Started: "Mon Oct  5 18:00:00 2026"}
	if !identityMatches(record, path, record.Started) || identityMatches(record, path, "Mon Oct  5 18:01:00 2026") {
		t.Fatal("reused PID with matching executable was accepted")
	}
}

func TestScriptEntrypointExists(t *testing.T) {
	path := filepath.Join("..", "..", "scripts", "ariel.sh")
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm()&0100 == 0 {
		t.Fatalf("one-command executable missing: %v", err)
	}
}

func TestStatusKeepsPINHiddenUntilExplicitShowPIN(t *testing.T) {
	root := t.TempDir()
	opts, _ := parseArgs([]string{"up", "local", "--listen", "127.0.0.1:18080", "--device-id", "test-mac", "--device-name", "Test Mac"})
	if _, err := prepareConfig(root, opts); err != nil {
		t.Fatal(err)
	}
	pin, _ := os.ReadFile(filepath.Join(runtimeDir(root), "web-pin"))
	var output bytes.Buffer
	if err := Run(context.Background(), root, []string{"status"}, &output); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), strings.TrimSpace(string(pin))) {
		t.Fatal("status disclosed Web PIN")
	}
	output.Reset()
	if err := Run(context.Background(), root, []string{"show-pin"}, &output); err != nil || strings.TrimSpace(output.String()) != strings.TrimSpace(string(pin)) {
		t.Fatalf("show-pin: %q, %v", output.String(), err)
	}
}

func TestAgentOnlyStopNeverTreatsRelayAsManaged(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "token")
	if err := os.WriteFile(source, []byte(strings.Repeat("a", 64)), 0600); err != nil {
		t.Fatal(err)
	}
	opts, _ := parseArgs([]string{"up", "agent", "--relay-url", "wss://relay.example/ws", "--device-id", "test-mac", "--device-name", "Test Mac", "--token-file", source})
	if _, err := prepareConfig(root, opts); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := Run(context.Background(), root, []string{"stop"}, &output); err != nil || strings.Contains(output.String(), "relay") {
		t.Fatalf("agent-only stop touched Relay: %q, %v", output.String(), err)
	}
}

func TestPortConflictFailsWithoutStartingOrStoppingForeignProcess(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	root := t.TempDir()
	opts, _ := parseArgs([]string{"up", "local", "--listen", listener.Addr().String(), "--device-id", "test-mac", "--device-name", "Test Mac"})
	if _, err := prepareConfig(root, opts); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := Run(context.Background(), root, []string{"up"}, &output); err == nil || !strings.Contains(err.Error(), "port") {
		t.Fatalf("expected port conflict, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(runtimeDir(root), "relay.pid")); !os.IsNotExist(err) {
		t.Fatal("port conflict created a managed process")
	}
}

func TestStopNeverSignalsUnownedPID(t *testing.T) {
	root := t.TempDir()
	opts, _ := parseArgs([]string{"up", "local", "--listen", "127.0.0.1:18080", "--device-id", "test-mac", "--device-name", "Test Mac"})
	if _, err := prepareConfig(root, opts); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(managedProcess{PID: os.Getpid(), Executable: filepath.Join(root, ".local", "bin", "ariel-relay")})
	if err := os.WriteFile(filepath.Join(runtimeDir(root), "relay.pid"), data, 0600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	_ = Run(context.Background(), root, []string{"stop"}, &output)
	if !strings.Contains(output.String(), "未管理") {
		t.Fatalf("foreign PID not reported: %q", output.String())
	}
}

func TestRollbackStopsOnlyNewManagedProcesses(t *testing.T) {
	root := t.TempDir()
	pids := []int{}
	if err := os.MkdirAll(runtimeDir(root), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".local", "bin"), 0700); err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join("..", "..", "tests", "fixtures", "startup", "hold.go")
	for _, name := range []string{"relay", "desktop-agent"} {
		if output, err := exec.Command("go", "build", "-o", executable(root, name), fixture).CombinedOutput(); err != nil {
			t.Fatalf("build fixture: %v: %s", err, output)
		}
		pid, err := startManaged(root, name, os.Environ())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGTERM) })
		pids = append(pids, pid)
	}
	if err := rollbackStarted(root, "local"); err != nil {
		for _, pid := range pids {
			state, _ := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "stat=", "-o", "command=").CombinedOutput()
			t.Logf("PID %d after stop: %q", pid, state)
		}
		t.Fatal(err)
	}
	for _, name := range []string{"relay", "desktop-agent"} {
		_, running, err := readManaged(root, name)
		if err != nil || running {
			t.Fatalf("%s survived rollback: %v, %v", name, running, err)
		}
	}
}
