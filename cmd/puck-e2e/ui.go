package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func (r *runner) scenarioUI(webRoot string) ScenarioResult {
	script := filepath.Join(webRoot, "e2e", "puck-headless.mjs")
	absScript, err := filepath.Abs(script)
	if err != nil {
		return passResult("UI", "headless browser", []Check{{Name: "playwright", Pass: false, Detail: err.Error()}})
	}
	absRoot, err := filepath.Abs(webRoot)
	if err != nil {
		return passResult("UI", "headless browser", []Check{{Name: "playwright", Pass: false, Detail: err.Error()}})
	}
	if _, err := os.Stat(absScript); err != nil {
		return passResult("UI", "headless browser", []Check{{Name: "playwright", Pass: false, Detail: "missing " + absScript}})
	}
	shots := filepath.Join(r.stack.iso.Root, "ui")
	profile := filepath.Join(shots, "profile")
	if err := os.MkdirAll(profile, 0o700); err != nil {
		return passResult("UI", "headless browser", []Check{{Name: "profile", Pass: false, Detail: "could not create a temp browser profile"}})
	}
	cmd := exec.CommandContext(r.ctx, "node", absScript)
	cmd.Dir = absRoot
	cmd.Env = append(os.Environ(),
		"PUCK_E2E_ORIGIN="+r.stack.origin,
		"PUCK_E2E_PIN="+r.stack.pin,
		"PUCK_E2E_PROFILE="+profile,
		"PUCK_E2E_SHOTS="+shots,
	)
	output, err := cmd.CombinedOutput()
	detail := tailText(string(output), 12)
	if strings.Contains(detail, r.stack.pin) {
		detail = strings.ReplaceAll(detail, r.stack.pin, "[pin]")
	}
	failedShot := filepath.Join(shots, "puck-chat-failed.png")
	if err != nil {
		shot := "screenshot=missing"
		if _, statErr := os.Stat(failedShot); statErr == nil {
			shot = "screenshot=" + failedShot
		}
		return passResult("UI", "headless browser", []Check{{Name: "puck-page", Pass: false, Detail: strings.TrimSpace(shot + " " + detail + " (" + err.Error() + ")")}})
	}
	return passResult("UI", "headless browser", []Check{{Name: "puck-page", Pass: true, Detail: detail + " shots=" + shots}})
}

func tailText(value string, lines int) string {
	value = strings.TrimSpace(value)
	parts := strings.Split(value, "\n")
	if len(parts) > lines {
		parts = parts[len(parts)-lines:]
	}
	return strings.Join(parts, "\n")
}
