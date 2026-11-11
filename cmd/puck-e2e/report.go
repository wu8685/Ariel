package main

import (
	"encoding/json"
	"strings"
)

type Check struct {
	Name   string `json:"name"`
	Pass   bool   `json:"pass"`
	Detail string `json:"detail,omitempty"`
}

type ScenarioResult struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Status   string  `json:"status"`
	Attempts int     `json:"attempts,omitempty"`
	Checks   []Check `json:"checks"`
}

type Report struct {
	OK         bool             `json:"ok"`
	HardFailed bool             `json:"hardFailed"`
	SoftFailed bool             `json:"softFailed"`
	Listen     string           `json:"listen,omitempty"`
	Isolation  Isolation        `json:"isolation"`
	Scenarios  []ScenarioResult `json:"scenarios"`
}

func (r *Report) add(result ScenarioResult) {
	for index := range result.Checks {
		result.Checks[index].Detail = redact(result.Checks[index].Detail)
	}
	r.Scenarios = append(r.Scenarios, result)
	switch result.Status {
	case "fail":
		r.HardFailed = true
	case "soft_fail":
		r.SoftFailed = true
	}
	r.OK = !r.HardFailed && !r.SoftFailed
}

func (r Report) exitCode() int {
	if r.HardFailed {
		return 1
	}
	if r.SoftFailed {
		return 3
	}
	return 0
}

func redact(value string) string {
	if strings.Contains(value, "[puck-handoff]") || strings.Contains(value, "【Referenced") || strings.Contains(value, "【系统上下文】") {
		return "[redacted]"
	}
	if len(value) > 240 && !strings.HasPrefix(value, "/") && !strings.HasPrefix(value, "codex://") && !strings.HasPrefix(value, "FIXTURE") {
		return "[redacted]"
	}
	return value
}

func (r Report) JSON() ([]byte, error) {
	return json.MarshalIndent(r, "", "  ")
}

func passResult(id, name string, checks []Check) ScenarioResult {
	status := "pass"
	for _, check := range checks {
		if !check.Pass {
			status = "fail"
			break
		}
	}
	return ScenarioResult{ID: id, Name: name, Status: status, Checks: checks}
}

func softResult(id, name string, attempts int, checks []Check) ScenarioResult {
	status := "soft_pass"
	for _, check := range checks {
		if !check.Pass {
			status = "soft_fail"
			break
		}
	}
	return ScenarioResult{ID: id, Name: name, Status: status, Attempts: attempts, Checks: checks}
}
