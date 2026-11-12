package main

import (
	"encoding/json"
	"regexp"
	"strings"
)

type Check struct {
	Name   string `json:"name"`
	Pass   bool   `json:"pass"`
	Detail string `json:"detail,omitempty"`
}

type ScenarioResult struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Status    string   `json:"status"`
	Attempts  int      `json:"attempts,omitempty"`
	Checks    []Check  `json:"checks"`
	ToolCalls []string `json:"toolCalls,omitempty"`
	WakeSkips []string `json:"wakeSkips,omitempty"`
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
	case "observed":
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

var (
	uuidPattern   = regexp.MustCompile(`(?i)[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)
	secretPattern = regexp.MustCompile(`(?i)(token|pin|password|secret|authorization)([=: ]+)(\S+)`)
)

func redact(value string) string {
	value = uuidPattern.ReplaceAllString(value, "…")
	value = secretPattern.ReplaceAllString(value, "$1$2[redacted]")
	value = stripSpan(value, "[puck-handoff]", "\n\n")
	value = stripSpan(value, "【Referenced", "\n\n")
	value = stripSpan(value, "【系统上下文】", "【机主自定义上下文")
	lines := strings.Split(value, "\n")
	kept := lines[:0]
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "at ") || strings.HasPrefix(trim, "goroutine ") || strings.Contains(trim, "node:internal") {
			continue
		}
		kept = append(kept, line)
	}
	value = strings.TrimSpace(strings.Join(kept, "\n"))
	if len(value) > 500 {
		value = value[:500] + "…"
	}
	return value
}

func stripSpan(value, start, end string) string {
	for {
		from := strings.Index(value, start)
		if from < 0 {
			return value
		}
		rest := value[from+len(start):]
		until := strings.Index(rest, end)
		if until < 0 {
			return value[:from] + "[body]"
		}
		value = value[:from] + "[body]" + rest[until:]
	}
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
