package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"math/rand"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/wu8685/Ariel/internal/desktopagent"
	"github.com/wu8685/Ariel/internal/obsidiantasks"
	"github.com/wu8685/Ariel/internal/probe"
)

type config struct {
	url, token, deviceID, deviceName, appPath, socket, chillingEndpoint string
	chillingEnabled                                                     bool
	tasksEnabled, tasksManagerOnly                                      bool
	taskEndpoint, obsidianTaskVault                                     string
	taskRoots, taskManagers                                             []string
}

type reconnectBackoff struct{ next time.Duration }

func newReconnectBackoff() *reconnectBackoff {
	return &reconnectBackoff{next: time.Second}
}

func (b *reconnectBackoff) connected() {
	b.next = time.Second
}

func (b *reconnectBackoff) nextDelay(random float64) time.Duration {
	current := b.next
	b.next = min(b.next*2, 15*time.Second)
	return time.Duration(float64(current) * (0.8 + random*0.4))
}

func configFrom(relayURL, token, deviceID, deviceName, appPath, socket string) (config, error) {
	if relayURL == "" || token == "" || deviceID == "" || deviceName == "" || appPath == "" || socket == "" {
		return config{}, errors.New("Relay URL, token, device ID/name, app path and IPC socket are required")
	}
	parsed, err := url.Parse(relayURL)
	if err != nil || (parsed.Scheme != "ws" && parsed.Scheme != "wss") || parsed.Host == "" || parsed.Path != "/ws" || parsed.RawQuery != "" {
		return config{}, errors.New("ARIEL_RELAY_URL must be ws(s)://host/ws")
	}
	return config{url: relayURL, token: token, deviceID: deviceID, deviceName: deviceName, appPath: appPath, socket: socket}, nil
}

func configureTasks(cfg config, enabled, managerOnly bool, endpoint, obsidianVault, roots, managers string) (config, error) {
	if !enabled {
		if managerOnly {
			return config{}, errors.New("--tasks-manager-only requires --enable-tasks")
		}
		return cfg, nil
	}
	cfg.tasksEnabled, cfg.tasksManagerOnly, cfg.taskEndpoint = enabled, managerOnly, endpoint
	if endpoint != "" && obsidianVault != "" {
		return config{}, errors.New("Chilling and Obsidian task backends are mutually exclusive")
	}
	if endpoint != "" {
		u, err := url.Parse(endpoint)
		if err != nil || u.Scheme != "unix" || u.Host != "" || !filepath.IsAbs(u.Path) || u.RawQuery != "" || u.Fragment != "" {
			return config{}, errors.New("--chilling-task-endpoint must be an absolute unix:// endpoint")
		}
	}
	if obsidianVault != "" {
		if !filepath.IsAbs(obsidianVault) {
			return config{}, errors.New("--obsidian-task-vault must be absolute")
		}
		resolved, err := filepath.EvalSymlinks(filepath.Clean(obsidianVault))
		if err != nil {
			return config{}, errors.New("--obsidian-task-vault is unavailable")
		}
		if info, err := os.Stat(filepath.Join(resolved, "tasks", "list")); err != nil || !info.IsDir() {
			return config{}, errors.New("--obsidian-task-vault must contain tasks/list")
		}
		cfg.obsidianTaskVault = resolved
	}
	if managerOnly && endpoint == "" && cfg.obsidianTaskVault == "" {
		return config{}, errors.New("manager-only mode requires a task backend")
	}
	for _, root := range strings.Split(roots, ",") {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		if !filepath.IsAbs(root) {
			return config{}, errors.New("task allowed roots must be absolute")
		}
		cfg.taskRoots = append(cfg.taskRoots, filepath.Clean(root))
	}
	for _, manager := range strings.Split(managers, ",") {
		manager = strings.TrimSpace(manager)
		if manager == "" {
			continue
		}
		if len(manager) > 128 {
			return config{}, errors.New("invalid task manager identity")
		}
		cfg.taskManagers = append(cfg.taskManagers, manager)
	}
	return cfg, nil
}

// A working CLI/account does not prove native Desktop ownership compatibility.
// nil keeps the separate official-provider check installed by desktopagent.Run;
// an unverified native version blocks task writes, never ordinary conversations.
func taskExecutionCheckForProfile(enabled, managerOnly bool, profile probe.IPCProfileStatus) func(context.Context, string, string) error {
	if !enabled || managerOnly || profile == probe.IPCProfileVerified {
		return nil
	}
	return func(context.Context, string, string) error {
		return errors.New("NATIVE_COMPATIBILITY_UNVERIFIED")
	}
}

func main() {
	enableChilling := flag.Bool("enable-chilling-file-context", os.Getenv("ARIEL_ENABLE_CHILLING_FILE_CONTEXT") == "true", "enable the local Chilling file-context gateway")
	chillingEndpoint := flag.String("chilling-endpoint", os.Getenv("ARIEL_CHILLING_ENDPOINT"), "Chilling unix:// endpoint")
	enableTasks := flag.Bool("enable-tasks", os.Getenv("ARIEL_ENABLE_TASKS") == "true", "enable task gateway / explicitly authorized task execution")
	managerOnly := flag.Bool("tasks-manager-only", os.Getenv("ARIEL_TASKS_MANAGER_ONLY") == "true", "run task management without a local Codex")
	taskEndpoint := flag.String("chilling-task-endpoint", os.Getenv("ARIEL_CHILLING_TASK_ENDPOINT"), "local Chilling task-service unix:// endpoint")
	obsidianTaskVault := flag.String("obsidian-task-vault", os.Getenv("ARIEL_OBSIDIAN_TASK_VAULT"), "absolute Obsidian vault path used as the task source")
	taskRoots := flag.String("task-allowed-roots", os.Getenv("ARIEL_TASK_ALLOWED_ROOTS"), "comma-separated absolute task execution roots")
	taskManagers := flag.String("task-allowed-managers", os.Getenv("ARIEL_TASK_ALLOWED_MANAGERS"), "comma-separated authorized manager device IDs")
	flag.Parse()
	defaults, err := probe.Defaults()
	if err != nil {
		log.Fatal(err)
	}
	if *enableChilling && *chillingEndpoint == "" {
		log.Fatal("--chilling-endpoint is required when Chilling file context is enabled")
	}
	appPath := os.Getenv("ARIEL_APP_PATH")
	if appPath == "" {
		appPath = defaults.AppPath
	}
	socket := os.Getenv("ARIEL_IPC_SOCKET")
	if socket == "" {
		socket = defaults.Socket
	}
	cfg, err := configFrom(os.Getenv("ARIEL_RELAY_URL"), os.Getenv("ARIEL_TOKEN"), os.Getenv("ARIEL_DEVICE_ID"), os.Getenv("ARIEL_DEVICE_NAME"), appPath, socket)
	if err != nil {
		log.Fatal(err)
	}
	cfg.chillingEnabled, cfg.chillingEndpoint = *enableChilling, *chillingEndpoint
	cfg, err = configureTasks(cfg, *enableTasks, *managerOnly, *taskEndpoint, *obsidianTaskVault, *taskRoots, *taskManagers)
	if err != nil {
		log.Fatal(err)
	}
	if cfg.tasksManagerOnly && cfg.chillingEnabled {
		log.Fatal("manager-only mode cannot provide local Codex file context")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var taskBackend desktopagent.TaskBackend
	if cfg.obsidianTaskVault != "" {
		taskBackend, err = obsidiantasks.New(obsidiantasks.Options{VaultRoot: cfg.obsidianTaskVault, DefaultHostID: cfg.deviceID})
		if err != nil {
			log.Fatal(err)
		}
	}
	var binary string
	var taskExecutionCheck func(context.Context, string, string) error
	if !cfg.tasksManagerOnly {
		desktopVersion, err := probe.DesktopVersion(ctx, cfg.appPath)
		if err != nil {
			log.Fatal("Desktop version unavailable")
		}
		selectedBinary, cliVersion, err := probe.SelectBinary(os.Getenv("ARIEL_CODEX_BINARY"), probe.BundledBinary(cfg.appPath), func(p string) (string, error) { return probe.BinaryVersion(ctx, p) })
		if err != nil {
			log.Fatal(err)
		}
		profileStatus, err := probe.CheckIPCProfile(desktopVersion, cliVersion)
		if err != nil {
			log.Fatal(err)
		}
		if profileStatus == probe.IPCProfileUnverified {
			log.Printf("compatibility warning: Desktop %s / Codex %s is above the minimum profile and has not been verified version-by-version; incompatible IPC operations will fail", desktopVersion, cliVersion)
		}
		taskExecutionCheck = taskExecutionCheckForProfile(cfg.tasksEnabled, cfg.tasksManagerOnly, profileStatus)
		if taskExecutionCheck != nil {
			log.Print("task writes disabled: native compatibility is unverified; management and read-only recovery remain available")
		}
		binary = selectedBinary
	}
	backoff := newReconnectBackoff()
	readyFile := os.Getenv("ARIEL_READY_FILE")
	if readyFile != "" {
		_ = os.Remove(readyFile)
	}
	markReady := func() error {
		if readyFile == "" {
			return nil
		}
		return os.WriteFile(readyFile, []byte(strconv.Itoa(os.Getpid())), 0600)
	}
	clearReady := func() {
		if readyFile != "" {
			_ = os.Remove(readyFile)
		}
	}
	defer clearReady()
	for ctx.Err() == nil {
		onReady := func() error {
			if err := markReady(); err != nil {
				return err
			}
			backoff.connected()
			return nil
		}
		err := desktopagent.Run(ctx, desktopagent.Config{URL: cfg.url, Token: cfg.token, DeviceID: cfg.deviceID, DeviceName: cfg.deviceName, Binary: binary, Socket: cfg.socket, ChillingEnabled: cfg.chillingEnabled, ChillingEndpoint: cfg.chillingEndpoint, TasksEnabled: cfg.tasksEnabled, TasksManagerOnly: cfg.tasksManagerOnly, TaskEndpoint: cfg.taskEndpoint, TaskBackend: taskBackend, TaskAllowedRoots: cfg.taskRoots, TaskAllowedManagers: cfg.taskManagers, TaskExecutionCheck: taskExecutionCheck, CachePrewarmRecent: true, OnReady: onReady, OnNotReady: clearReady})
		if ctx.Err() != nil {
			break
		}
		log.Printf("Desktop Agent disconnected: %v; reconnecting", err)
		wait := backoff.nextDelay(rand.Float64())
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}
