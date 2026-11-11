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
	var root, binary, socket, only, jsonPath string
	var retries int
	var timeout time.Duration
	var keep bool
	flag.StringVar(&root, "state", "", "empty temp directory; default is a new directory under the system temp dir")
	flag.StringVar(&binary, "codex-binary", os.Getenv("ARIEL_CODEX_BINARY"), "Codex app-server binary; default is the Desktop bundle")
	flag.StringVar(&socket, "ipc-socket", os.Getenv("ARIEL_IPC_SOCKET"), "Codex Desktop IPC socket; config.toml is never opened")
	flag.StringVar(&only, "only", "", "comma-separated scenario ids, for example A,B,F")
	flag.IntVar(&retries, "llm-retries", 2, "extra attempts for model-dependent scenarios D and G")
	flag.DurationVar(&timeout, "timeout", 45*time.Minute, "overall deadline")
	flag.BoolVar(&keep, "keep", false, "keep the temp directory after the run")
	flag.StringVar(&jsonPath, "json", "", "write the JSON report to this path in addition to stdout")
	flag.Parse()

	if binary == "" || socket == "" {
		defaults, err := probe.Defaults()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		if binary == "" {
			binary = probe.BundledBinary(defaults.AppPath)
		}
		if socket == "" {
			socket = defaults.Socket
		}
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
	report, err := execute(ctx, options{Root: root, Binary: binary, Socket: socket, Only: selected, Retries: retries, Timeout: timeout, Keep: keep})
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
