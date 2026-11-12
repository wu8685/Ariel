package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/wu8685/Ariel/internal/probe"
)

func main() {
	var root, binary, socket, only, jsonPath, webDist string
	var retries int
	var timeout time.Duration
	var keep, ui, fake bool
	flag.StringVar(&root, "state", "", "empty temp directory; default is a new directory under the system temp dir")
	flag.StringVar(&binary, "codex-binary", os.Getenv("ARIEL_CODEX_BINARY"), "Codex CLI binary used as a private app-server; default is the Desktop bundle")
	flag.StringVar(&socket, "ipc-socket", "", "rejected; this harness never attaches to the Desktop IPC socket")
	flag.StringVar(&only, "only", "", "comma-separated scenario ids, for example A,B,F")
	flag.IntVar(&retries, "llm-retries", 2, "extra attempts for model-dependent scenario D")
	flag.DurationVar(&timeout, "timeout", 45*time.Minute, "overall deadline")
	flag.BoolVar(&keep, "keep", false, "keep the temp directory, including UI screenshots, after the run")
	flag.BoolVar(&ui, "ui", false, "after the chat scenarios, open the isolated Relay in headless Chromium")
	flag.BoolVar(&fake, "fake", false, "use the deterministic fake codex app-server instead of a real Codex CLI")
	flag.StringVar(&webDist, "web-dist", "web/dist", "built web assets served when -ui is set")
	flag.StringVar(&jsonPath, "json", "", "write the JSON report to this path in addition to stdout")
	flag.Parse()
	if socket != "" || os.Getenv("ARIEL_IPC_SOCKET") != "" {
		fmt.Fprintln(os.Stderr, "refusing Desktop IPC: puck-e2e is headless and will not attach to Codex Desktop. Unset ARIEL_IPC_SOCKET and do not pass -ipc-socket.")
		os.Exit(2)
	}
	if binary == "" && !fake {
		defaults, err := probe.Defaults()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		binary = probe.BundledBinary(defaults.AppPath)
	}
	selected := map[string]bool{}
	for _, id := range strings.Split(only, ",") {
		id = strings.TrimSpace(id)
		if id != "" {
			selected[id] = true
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	report, err := execute(ctx, options{Root: root, Binary: binary, Only: selected, Retries: retries, Timeout: timeout, Keep: keep, UI: ui, Fake: fake, WebRoot: "web", WebDist: webDist})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	body, err := report.JSON()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	fmt.Println(string(body))
	if jsonPath != "" {
		if err := os.WriteFile(jsonPath, append(body, '\n'), 0o600); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
	}
	os.Exit(report.exitCode())
}
