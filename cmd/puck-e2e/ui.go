package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func (r *runner) scenarioUI(webRoot string) ScenarioResult {
	script := filepath.Join(webRoot, "e2e", "puck-headless.mjs")
	if _, err := os.Stat(script); err != nil {
		return passResult("UI", "headless browser", []Check{{Name: "playwright", Pass: false, Detail: "missing " + script}})
	}
	shots := filepath.Join(r.stack.iso.Root, "ui")
	profile := filepath.Join(shots, "profile")
	if err := os.MkdirAll(profile, 0o700); err != nil {
		return passResult("UI", "headless browser", []Check{{Name: "profile", Pass: false, Detail: "could not create a temp browser profile"}})
	}
	cmd := exec.CommandContext(r.ctx, "node", script)
	cmd.Dir = webRoot
	cmd.Env = append(os.Environ(),
		"PUCK_E2E_ORIGIN="+r.stack.origin,
		"PUCK_E2E_PIN="+r.stack.pin,
		"PUCK_E2E_PROFILE="+profile,
		"PUCK_E2E_SHOTS="+shots,
	)
	output, err := cmd.CombinedOutput()
	detail := strings.TrimSpace(string(output))
	if strings.Contains(detail, r.stack.pin) {
		detail = strings.ReplaceAll(detail, r.stack.pin, "[pin]")
	}
	if len(detail) > 240 {
		detail = detail[:240]
	}
	if err != nil {
		return passResult("UI", "headless browser", []Check{{Name: "puck-page", Pass: false, Detail: fmt.Sprintf("%s (%s)", detail, err)}})
	}
	return passResult("UI", "headless browser", []Check{{Name: "puck-page", Pass: true, Detail: detail + " shots=" + shots}})
}
