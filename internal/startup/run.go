package startup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/wu8685/Ariel/internal/probe"
)

const helpText = `Ariel one-command setup (macOS)

  scripts/ariel.sh up local --listen <LAN-IP:port> --device-id <id> --device-name <name> [--token-file <path>] [--pin-file <path>]
  scripts/ariel.sh up agent --relay-url <wss://host/ws> --device-id <id> --device-name <name> --token-file <path> [--allow-insecure-ws]
  scripts/ariel.sh up                  # start with saved configuration
  scripts/ariel.sh restart-local       # discover current private LAN IPv4 and safely restart local mode
  scripts/ariel.sh status              # no credentials shown
  scripts/ariel.sh stop                # only script-managed processes
  scripts/ariel.sh show-pin            # local mode only

To change saved settings, stop first and add --replace-config to a complete up local/agent command.
Credentials are imported from files, never command-line values.
`

type managedProcess struct {
	PID        int    `json:"pid"`
	Executable string `json:"executable"`
	Started    string `json:"started"`
}

func Run(ctx context.Context, root string, args []string, output io.Writer) error {
	opts, err := parseArgs(args)
	if err != nil {
		return err
	}
	switch opts.command {
	case "help":
		_, _ = io.WriteString(output, helpText)
		return nil
	case "show-pin":
		cfg, err := loadConfig(root)
		if err != nil {
			return err
		}
		if cfg.Mode != "local" {
			return errors.New("show-pin is only available in local mode")
		}
		pin, err := readCredential(filepath.Join(runtimeDir(root), "web-pin"), false)
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintln(output, pin)
		return nil
	case "status":
		return status(root, output)
	case "stop":
		return stop(root, output)
	case "restart-local":
		return restartLocal(ctx, root, output)
	case "up":
		return up(ctx, root, opts, output)
	default:
		return errors.New("unknown command")
	}
}

func executable(root, name string) string {
	return filepath.Join(root, ".local", "bin", "ariel-"+name)
}

func pidPath(root, name string) string { return filepath.Join(runtimeDir(root), name+".pid") }
func readyPath(root string) string     { return filepath.Join(runtimeDir(root), "agent.ready") }

func readManaged(root, name string) (managedProcess, bool, error) {
	data, err := os.ReadFile(pidPath(root, name))
	if os.IsNotExist(err) {
		return managedProcess{}, false, nil
	}
	if err != nil {
		return managedProcess{}, false, err
	}
	var record managedProcess
	if err := json.Unmarshal(data, &record); err != nil || record.PID < 1 || record.Executable != executable(root, name) || record.Started == "" {
		return managedProcess{}, false, fmt.Errorf("%s PID record is invalid; refusing to signal", name)
	}
	state, err := exec.Command("ps", "-p", strconv.Itoa(record.PID), "-o", "stat=").Output()
	if err != nil || strings.HasPrefix(strings.TrimSpace(string(state)), "Z") {
		_ = os.Remove(pidPath(root, name))
		return record, false, nil
	}
	actual, err := exec.Command("ps", "-p", strconv.Itoa(record.PID), "-o", "command=").Output()
	if err != nil {
		_ = os.Remove(pidPath(root, name))
		return record, false, nil
	}
	started, err := exec.Command("ps", "-p", strconv.Itoa(record.PID), "-o", "lstart=").Output()
	if err != nil {
		_ = os.Remove(pidPath(root, name))
		return record, false, nil
	}
	if !identityMatches(record, string(actual), string(started)) {
		return record, false, fmt.Errorf("%s PID belongs to an unmanaged process; refusing to signal", name)
	}
	return record, true, nil
}

func identityMatches(record managedProcess, command, started string) bool {
	return commandMatches(record.Executable, command) && record.Started == strings.TrimSpace(started)
}

func managedState(root string) (relay, agent bool, err error) {
	_, relay, err = readManaged(root, "relay")
	if err != nil {
		return false, false, err
	}
	_, agent, err = readManaged(root, "desktop-agent")
	return relay, agent, err
}

func stopOne(root, name string, output io.Writer) error {
	record, running, err := readManaged(root, name)
	if err != nil {
		_, _ = fmt.Fprintf(output, "%s 未管理：%v\n", name, err)
		return err
	}
	if !running {
		_ = os.Remove(pidPath(root, name))
		_, _ = fmt.Fprintf(output, "%s 已停止\n", name)
		return nil
	}
	process, err := os.FindProcess(record.PID)
	if err != nil {
		return err
	}
	if err := process.Signal(syscall.SIGTERM); err != nil {
		return err
	}
	for until := time.Now().Add(6 * time.Second); time.Now().Before(until); time.Sleep(100 * time.Millisecond) {
		_, alive, err := readManaged(root, name)
		if err == nil && !alive {
			_ = os.Remove(pidPath(root, name))
			_, _ = fmt.Fprintf(output, "%s 已停止\n", name)
			return nil
		}
	}
	return fmt.Errorf("%s did not stop after SIGTERM; no forced kill was sent", name)
}

func stop(root string, output io.Writer) error {
	cfg, err := loadConfig(root)
	if err != nil {
		return fmt.Errorf("no saved configuration: %w", err)
	}
	first := stopOne(root, "desktop-agent", output)
	var second error
	if cfg.Mode == "local" {
		second = stopOne(root, "relay", output)
	}
	_ = os.Remove(readyPath(root))
	if first != nil {
		return first
	}
	return second
}

func rollbackStarted(root, mode string) error {
	agentErr := stopOne(root, "desktop-agent", io.Discard)
	var relayErr error
	if mode == "local" {
		relayErr = stopOne(root, "relay", io.Discard)
	}
	_ = os.Remove(readyPath(root))
	return errors.Join(agentErr, relayErr)
}

func httpReady(listen string) bool {
	transport := &http.Transport{Proxy: nil}
	defer transport.CloseIdleConnections()
	client := &http.Client{Timeout: 2 * time.Second, Transport: transport}
	resp, err := client.Get("http://" + listen + "/healthz")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func agentReady(root string, pid int) bool {
	data, err := os.ReadFile(readyPath(root))
	return err == nil && strings.TrimSpace(string(data)) == strconv.Itoa(pid)
}

func status(root string, output io.Writer) error {
	cfg, err := loadConfig(root)
	if err != nil {
		return fmt.Errorf("no saved configuration; run up local or up agent: %w", err)
	}
	if cfg.Mode == "local" {
		_, running, err := readManaged(root, "relay")
		if err != nil {
			_, _ = fmt.Fprintf(output, "Relay 未管理：%v\n", err)
		} else if running && httpReady(cfg.Listen) {
			_, _ = fmt.Fprintf(output, "Relay 在线：http://%s/\n", cfg.Listen)
		} else {
			_, _ = fmt.Fprintln(output, "Relay 离线")
		}
	}
	record, running, err := readManaged(root, "desktop-agent")
	if err != nil {
		_, _ = fmt.Fprintf(output, "Agent 未管理：%v\n", err)
	} else if !running {
		_, _ = fmt.Fprintln(output, "Agent 离线")
	} else if agentReady(root, record.PID) {
		_, _ = fmt.Fprintln(output, "Agent 已与 Relay 握手，Codex 就绪")
	} else {
		_, _ = fmt.Fprintln(output, "Agent 进程运行，Relay 握手尚未核实")
	}
	return nil
}

func checkPortFree(listen string) error {
	listener, err := net.Listen("tcp", listen)
	if err != nil {
		return fmt.Errorf("port %s is occupied or unavailable; existing services will not be stopped: %w", listen, err)
	}
	return listener.Close()
}

func checkDesktop(ctx context.Context) (probe.Environment, probe.IPCProfileStatus, error) {
	defaults, err := probe.Defaults()
	if err != nil {
		return probe.Environment{}, probe.IPCProfileUnsupported, err
	}
	env, err := probe.Detect(ctx, defaults)
	status, profileErr := probe.CheckIPCProfile(env.DesktopVersion, env.BundledCodexVersion)
	if profileErr != nil {
		return env, status, profileErr
	}
	if err != nil || !env.IPCInitialized {
		return env, probe.IPCProfileUnsupported, errors.New("Codex Desktop IPC is unavailable; open the compatible Desktop app first")
	}
	return env, status, nil
}

func runBuild(ctx context.Context, root, name string, output io.Writer) error {
	var cmd *exec.Cmd
	switch name {
	case "npm ci":
		cmd = exec.CommandContext(ctx, "npm", "ci")
		cmd.Dir = filepath.Join(root, "web")
	case "npm run build":
		cmd = exec.CommandContext(ctx, "npm", "run", "build")
		cmd.Dir = filepath.Join(root, "web")
	case "go build relay", "go build desktop-agent":
		component := "relay"
		if name == "go build desktop-agent" {
			component = "desktop-agent"
		}
		binDir := filepath.Join(root, ".local", "bin")
		if err := os.MkdirAll(binDir, 0700); err != nil {
			return err
		}
		tmp := filepath.Join(binDir, ".ariel-"+component+"-build")
		defer os.Remove(tmp)
		cmd = exec.CommandContext(ctx, "go", "build", "-o", tmp, "./cmd/"+component)
		cmd.Dir = root
		cmd.Env = cleanArielEnvironment(os.Environ())
		cmd.Stdout, cmd.Stderr = output, output
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("%s failed: %w", name, err)
		}
		return os.Rename(tmp, executable(root, component))
	default:
		return errors.New("unknown build step")
	}
	cmd.Env = cleanArielEnvironment(os.Environ())
	cmd.Stdout, cmd.Stderr = output, output
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s failed: %w", name, err)
	}
	return nil
}

func cleanArielEnvironment(base []string) []string {
	out := make([]string, 0, len(base))
	for _, entry := range base {
		if !strings.HasPrefix(strings.SplitN(entry, "=", 2)[0], "ARIEL_") {
			out = append(out, entry)
		}
	}
	return out
}

func envWith(base []string, entries ...string) []string {
	keys := map[string]bool{}
	for _, entry := range entries {
		keys[strings.SplitN(entry, "=", 2)[0]] = true
	}
	out := make([]string, 0, len(base)+len(entries))
	for _, entry := range base {
		if !keys[strings.SplitN(entry, "=", 2)[0]] {
			out = append(out, entry)
		}
	}
	return append(out, entries...)
}

func startManaged(root, name string, env []string) (int, error) {
	path := executable(root, name)
	log, err := os.OpenFile(filepath.Join(runtimeDir(root), name+".log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return 0, err
	}
	defer log.Close()
	cmd := exec.Command(path)
	cmd.Dir = root
	cmd.Env = env
	cmd.Stdout, cmd.Stderr = log, log
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return 0, err
	}
	pid := cmd.Process.Pid
	var started string
	for attempts := 0; attempts < 10; attempts++ {
		stamp, stampErr := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "lstart=").Output()
		if stampErr == nil {
			started = strings.TrimSpace(string(stamp))
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if started == "" {
		_ = cmd.Process.Kill()
		_ = cmd.Process.Release()
		return 0, fmt.Errorf("%s exited before PID identity could be recorded", name)
	}
	_ = cmd.Process.Release()
	data, _ := json.Marshal(managedProcess{PID: pid, Executable: path, Started: started})
	if err := writePrivate(pidPath(root, name), data); err != nil {
		_ = syscall.Kill(pid, syscall.SIGTERM)
		return 0, err
	}
	return pid, nil
}

func waitForReady(root, name string, pid int, ready func() bool) error {
	for until := time.Now().Add(20 * time.Second); time.Now().Before(until); time.Sleep(200 * time.Millisecond) {
		record, running, err := readManaged(root, name)
		if err != nil {
			return fmt.Errorf("%s failed its managed-process identity check before readiness: %w", name, err)
		}
		if !running || record.PID != pid {
			return fmt.Errorf("%s exited before readiness; inspect .local/runtime/%s.log", name, name)
		}
		if ready() {
			return nil
		}
	}
	return fmt.Errorf("%s did not become ready; inspect .local/runtime/%s.log", name, name)
}

func buildConfigured(ctx context.Context, root string, cfg config, output io.Writer) error {
	env, profileStatus, err := checkDesktop(ctx)
	if err != nil {
		return err
	}
	if profileStatus == probe.IPCProfileUnverified {
		_, _ = fmt.Fprintf(output, "兼容性提示：Desktop %s / Codex %s 高于最低版本，允许启动但尚未逐版本验证；若私有 IPC 已变化，当前操作会显式失败。\n", env.DesktopVersion, env.BundledCodexVersion)
	}
	if _, err := exec.LookPath("go"); err != nil {
		return errors.New("Go 1.26+ is required")
	}
	if cfg.Mode == "local" {
		if _, err := exec.LookPath("npm"); err != nil {
			return errors.New("Node.js/npm is required for local mode")
		}
	}
	for _, step := range buildSteps(cfg) {
		_, _ = fmt.Fprintf(output, "构建：%s\n", step)
		if err := runBuild(ctx, root, step, output); err != nil {
			return err
		}
	}
	return nil
}

func startConfigured(root string, cfg config, output io.Writer) error {
	token, err := readCredential(filepath.Join(runtimeDir(root), "agent-token"), true)
	if err != nil {
		return err
	}
	if cfg.Mode == "local" {
		pin, err := readCredential(filepath.Join(runtimeDir(root), "web-pin"), false)
		if err != nil {
			return err
		}
		relayPID, err := startManaged(root, "relay", envWith(cleanArielEnvironment(os.Environ()), "ARIEL_TOKEN="+token, "ARIEL_WEB_PIN="+pin, "ARIEL_LISTEN="+cfg.Listen, "ARIEL_ORIGINS=http://"+cfg.Listen, "ARIEL_WEB_DIST="+filepath.Join(root, "web", "dist")))
		if err != nil {
			return err
		}
		if err := waitForReady(root, "relay", relayPID, func() bool { return httpReady(cfg.Listen) }); err != nil {
			return errors.Join(err, rollbackStarted(root, cfg.Mode))
		}
	}
	_ = os.Remove(readyPath(root))
	agentEnv := cleanArielEnvironment(os.Environ())
	if cfg.Mode == "local" {
		host, _, _ := net.SplitHostPort(cfg.Listen)
		agentEnv = localAgentEnvironment(agentEnv, host)
	}
	agentEnv = envWith(agentEnv, "ARIEL_TOKEN="+token, "ARIEL_RELAY_URL="+cfg.RelayURL, "ARIEL_DEVICE_ID="+cfg.DeviceID, "ARIEL_DEVICE_NAME="+cfg.DeviceName, "ARIEL_READY_FILE="+readyPath(root))
	agentPID, err := startManaged(root, "desktop-agent", agentEnv)
	if err != nil {
		return errors.Join(err, rollbackStarted(root, cfg.Mode))
	}
	if err := waitForReady(root, "desktop-agent", agentPID, func() bool { return agentReady(root, agentPID) }); err != nil {
		return errors.Join(err, rollbackStarted(root, cfg.Mode))
	}
	if cfg.Mode == "local" {
		_, _ = fmt.Fprintf(output, "Ariel 可使用：http://%s/；运行 show-pin 可查看连接码\n", cfg.Listen)
	} else {
		_, _ = fmt.Fprintln(output, "Agent 已与指定 Relay 握手；请在该 Relay 的 Web 页面确认设备列表")
	}
	return nil
}

type restartLocalOps struct {
	discover     func(context.Context) (string, error)
	managedState func(string) (bool, bool, error)
	checkPort    func(string) error
	build        func(context.Context, string, config, io.Writer) error
	stop         func(string, io.Writer) error
	start        func(string, config, io.Writer) error
	status       func(string, io.Writer) error
}

func restartLocal(ctx context.Context, root string, output io.Writer) error {
	return restartLocalWith(ctx, root, output, restartLocalOps{
		discover:     func(ctx context.Context) (string, error) { return discoverPrivateLANIPv4(ctx, systemCommandOutput) },
		managedState: managedState,
		checkPort:    checkPortFree,
		build:        buildConfigured,
		stop:         stop,
		start:        startConfigured,
		status:       status,
	})
}

func restartLocalWith(ctx context.Context, root string, output io.Writer, ops restartLocalOps) error {
	cfg, err := loadConfig(root)
	if err != nil {
		return fmt.Errorf("restart-local requires an existing local configuration: %w", err)
	}
	if cfg.Mode != "local" {
		return errors.New("restart-local requires a saved local mode configuration")
	}
	address, err := ops.discover(ctx)
	if err != nil {
		return err
	}
	_, port, err := net.SplitHostPort(cfg.Listen)
	if err != nil {
		return errors.New("saved local listen address is invalid")
	}
	listen := net.JoinHostPort(address, port)
	candidate, err := localConfigForListen(cfg, listen)
	if err != nil {
		return err
	}
	relayRunning, agentRunning, err := ops.managedState(root)
	if err != nil {
		return err
	}
	if listen != cfg.Listen || !relayRunning {
		if err := ops.checkPort(listen); err != nil {
			return err
		}
	}
	if err := ops.build(ctx, root, candidate, output); err != nil {
		return err
	}
	if relayRunning || agentRunning {
		if err := ops.stop(root, output); err != nil {
			return err
		}
	}
	updated, err := updateLocalListen(root, cfg, listen)
	if err != nil {
		return err
	}
	if err := ops.start(root, updated, output); err != nil {
		return err
	}
	return ops.status(root, output)
}

func up(ctx context.Context, root string, opts options, output io.Writer) error {
	var cfg config
	var err error
	if opts.mode == "" {
		cfg, err = loadConfig(root)
		if err != nil {
			return fmt.Errorf("no saved configuration; run up local or up agent: %w", err)
		}
	} else {
		if opts.replace {
			relay, agent, err := managedState(root)
			if err != nil {
				return err
			}
			if relay || agent {
				return errors.New("stop managed services before --replace-config")
			}
		}
		cfg, err = prepareConfig(root, opts)
		if err != nil {
			return err
		}
	}
	relayRunning, agentRunning, err := managedState(root)
	if err != nil {
		return err
	}
	if agentRunning && (cfg.Mode == "agent" || relayRunning) {
		_, _ = fmt.Fprintln(output, "Ariel 已运行；如需更新二进制，请先 stop 再 up")
		return status(root, output)
	}
	if relayRunning || agentRunning {
		return errors.New("partial managed service state; run stop before up")
	}
	if cfg.Mode == "local" {
		if err := checkPortFree(cfg.Listen); err != nil {
			return err
		}
	}
	if err := buildConfigured(ctx, root, cfg, output); err != nil {
		return err
	}
	return startConfigured(root, cfg, output)
}
