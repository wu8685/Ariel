package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/wu8685/Ariel/internal/codex/appserver"
	"github.com/wu8685/Ariel/internal/probe"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	cancel()
	os.Exit(code)
}

const usage = `Ariel read-only compatibility probe
  probe detect [--app PATH] [--codex-binary PATH] [--ipc-socket PATH]
  probe history [--thread ID] [--codex-binary PATH] [--app PATH]
  probe owner --thread ID [--watch 2s] [--ipc-socket PATH]
  probe fixture create --manifest PATH --write-enabled [--workspace PARENT] [--purpose user-input]
  probe fixture exercise --manifest PATH --write-enabled
  probe fixture stale-interaction --manifest PATH --write-enabled
  probe fixture user-input --manifest PATH --write-enabled
  probe fixture user-input-race --manifest PATH --write-enabled
  probe fixture user-input-interrupt --manifest PATH --write-enabled
  probe fixture create --purpose command-decline --manifest PATH --write-enabled
  probe fixture command-decline --manifest PATH --write-enabled
  probe fixture create --purpose command-cancel --manifest PATH --write-enabled
  probe fixture command-cancel --manifest PATH --write-enabled
  probe fixture busy-submit --manifest PATH --write-enabled
Fixture create is opt-in: creates a new read-only isolated thread and a short seed reply.
Output contains structural summaries only; no chat text or live identifiers.
`

func run(ctx context.Context, args []string, out, errout io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(errout, usage)
		return 2
	}
	if args[0] == "help" || args[0] == "--help" {
		fmt.Fprint(out, usage)
		return 0
	}
	if args[0] == "fixture" {
		return fixtureCommand(ctx, args[1:], out, errout)
	}
	if args[0] != "detect" && args[0] != "history" && args[0] != "owner" {
		fmt.Fprintln(errout, "unknown probe operation")
		return 2
	}
	cfg, err := probe.Defaults()
	if err != nil {
		fmt.Fprintln(errout, "cannot determine home directory")
		return 1
	}
	f := flag.NewFlagSet("probe", flag.ContinueOnError)
	f.SetOutput(errout)
	f.StringVar(&cfg.AppPath, "app", cfg.AppPath, "Desktop application bundle")
	f.StringVar(&cfg.Binary, "codex-binary", "", "explicit binary, must match Desktop version")
	f.StringVar(&cfg.Socket, "ipc-socket", cfg.Socket, "Desktop IPC socket")
	thread := f.String("thread", "", "explicit read-only thread target")
	watch := f.Duration("watch", 2*time.Second, "observation duration, >0 and <=30s")
	if f.Parse(args[1:]) != nil || f.NArg() != 0 {
		return 2
	}
	if *watch <= 0 || *watch > 30*time.Second || (args[0] == "owner" && *thread == "") {
		fmt.Fprintln(errout, "owner requires --thread; --watch must be >0 and <=30s")
		return 2
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	var result any
	switch args[0] {
	case "detect":
		result, err = probe.Detect(ctx, cfg)
	case "history":
		result, err = history(ctx, cfg, *thread)
	case "owner":
		// Check binary compatibility before using the inspected private profile.
		_, version, e := probe.SelectBinary(cfg.Binary, probe.BundledBinary(cfg.AppPath), func(p string) (string, error) { return probe.BinaryVersion(ctx, p) })
		if e != nil {
			err = e
			break
		}
		desktopVersion, _ := probe.DesktopVersion(ctx, cfg.AppPath)
		if !probe.IPCProfileVerified(desktopVersion, version) {
			fmt.Fprintln(errout, "Desktop IPC version profile unverified")
			return 1
		}
		c, e := probe.ConnectThread(ctx, cfg.Socket, *thread)
		if e != nil {
			err = e
			break
		}
		defer c.Close()
		result, err = c.Observe(ctx, *thread, *watch)
	}
	status := "supported"
	if err != nil {
		status = "failed"
	}
	envelope := map[string]any{"capability": args[0], "status": status, "result": result}
	if err != nil {
		envelope["reason"] = err.Error()
	}
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	if encoder.Encode(envelope) != nil {
		return 1
	}
	if err != nil {
		return 1
	}
	return 0
}

func history(ctx context.Context, cfg probe.Config, threadID string) (any, error) {
	binary, version, err := probe.SelectBinary(cfg.Binary, probe.BundledBinary(cfg.AppPath), func(p string) (string, error) { return probe.BinaryVersion(ctx, p) })
	if err != nil {
		return nil, err
	}
	c, err := appserver.Start(ctx, binary, os.TempDir())
	if err != nil {
		return nil, err
	}
	defer c.Close()
	h := appserver.HistoryReader{RPC: c}
	first, err := h.List(ctx, "", 2)
	if err != nil {
		return nil, err
	}
	summary := map[string]any{"binaryVersion": version, "firstPageCount": len(first.Data), "nextPageAvailable": first.NextCursor != nil, "runtimeState": "unknown"}
	if first.NextCursor != nil {
		second, err := h.List(ctx, *first.NextCursor, 2)
		if err != nil {
			return summary, err
		}
		seen := map[string]bool{}
		for _, t := range first.Data {
			seen[t.ID] = true
		}
		duplicates := 0
		for _, t := range second.Data {
			if seen[t.ID] {
				duplicates++
			}
		}
		summary["secondPageCount"] = len(second.Data)
		summary["crossPageDuplicates"] = duplicates
	}
	if threadID != "" {
		thread, err := h.Read(ctx, threadID)
		if err != nil {
			return summary, err
		}
		summary["threadIDMatches"] = thread.ID == threadID
		summary["turnCount"] = len(thread.Turns)
		statuses := map[string]int{}
		itemCount := 0
		for _, turn := range thread.Turns {
			statuses[turn.Status]++
			itemCount += len(turn.Items)
		}
		summary["turnStatuses"] = statuses
		summary["itemCount"] = itemCount
	}
	return summary, nil
}
