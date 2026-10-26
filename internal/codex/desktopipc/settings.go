package desktopipc

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/wu8685/Ariel/internal/modelsettings"
)

// These writes were exercised on isolated fixtures, including the actual next
// turn and queued turn params. A generic IPC minimum-version check is not enough.
func SettingsProfileSupported(desktop, cli string) bool {
	return strings.TrimPrefix(desktop, "v") == "26.930.51102" && strings.TrimPrefix(cli, "v") == "0.160.0"
}

func projectExecutionMetadata(state map[string]any) json.RawMessage {
	take := func(source map[string]any, limits map[string]int) map[string]any {
		out := map[string]any{}
		for k, limit := range limits {
			v, ok := source[k]
			if !ok {
				continue
			}
			if v == nil {
				out[k] = nil
			} else if text, ok := v.(string); ok && len(text) <= limit {
				out[k] = text
			}
		}
		return out
	}
	out := take(state, map[string]int{"latestModel": 256, "latestReasoningEffort": 128})
	if s, ok := state["latestThreadSettings"].(map[string]any); ok {
		out["latestThreadSettings"] = take(s, map[string]int{"model": 256, "effort": 128, "serviceTier": 128})
	}
	if m, ok := state["latestCollaborationMode"].(map[string]any); ok {
		if s, ok := m["settings"].(map[string]any); ok {
			out["latestCollaborationMode"] = map[string]any{"settings": take(s, map[string]int{"model": 256, "reasoning_effort": 128})}
		}
	}
	b, _ := json.Marshal(out)
	if len(b) > 4096 {
		return nil
	}
	return b
}

// ExecutionSettings projects only next-turn settings, never active-turn params.
func ExecutionSettings(raw json.RawMessage) (modelsettings.Values, error) {
	var s struct {
		LatestModel  string                     `json:"latestModel"`
		LatestEffort string                     `json:"latestReasoningEffort"`
		Settings     map[string]json.RawMessage `json:"latestThreadSettings"`
		Mode         struct {
			Settings struct {
				Model  string `json:"model"`
				Effort string `json:"reasoning_effort"`
			} `json:"settings"`
		} `json:"latestCollaborationMode"`
	}
	if json.Unmarshal(raw, &s) != nil {
		return modelsettings.Values{}, ErrProtocol
	}
	v := modelsettings.Values{Model: s.LatestModel, Effort: s.LatestEffort, Speed: modelsettings.Speed{Mode: "inherit"}}
	if v.Model == "" {
		v.Model = s.Mode.Settings.Model
	}
	if v.Effort == "" {
		v.Effort = s.Mode.Settings.Effort
	}
	if b, ok := s.Settings["model"]; ok && string(b) != "null" {
		if json.Unmarshal(b, &v.Model) != nil {
			return v, ErrProtocol
		}
	}
	if b, ok := s.Settings["effort"]; ok {
		v.Effort = ""
		if json.Unmarshal(b, &v.Effort) != nil {
			return v, ErrProtocol
		}
	}
	if b, ok := s.Settings["serviceTier"]; ok {
		var tier *string
		if json.Unmarshal(b, &tier) != nil {
			return v, ErrProtocol
		}
		if tier != nil {
			if *tier == "default" {
				v.Speed.Mode = "standard"
			} else {
				v.Speed = modelsettings.Speed{Mode: "tier", TierID: *tier}
			}
		}
	}
	if v.Model == "" || len(v.Model) > 256 || len(v.Effort) > 128 || len(v.Speed.TierID) > 128 {
		return v, ErrProtocol
	}
	return v, nil
}

func UpdateExecutionSettings(ctx context.Context, c caller, owner, threadID, cwd string, raw json.RawMessage, v modelsettings.Values) error {
	var identity struct {
		CWD    string  `json:"cwd"`
		Effort *string `json:"latestReasoningEffort"`
	}
	if owner == "" || threadID == "" || cwd == "" || json.Unmarshal(raw, &identity) != nil || identity.CWD != cwd || v.Model == "" {
		return ErrProtocol
	}
	old, err := ExecutionSettings(raw)
	if err != nil {
		return err
	}
	var tier any
	switch v.Speed.Mode {
	case "inherit":
		tier = nil
	case "standard":
		tier = "default"
	case "tier":
		if v.Speed.TierID == "" {
			return ErrProtocol
		}
		tier = v.Speed.TierID
	default:
		return ErrProtocol
	}
	var effort any
	if v.Effort != "" {
		effort = v.Effort
	}
	r, err := c.Call(ctx, Request{Method: "thread-follower-update-thread-settings", Version: 2, TargetClientID: owner, Mutating: true, Params: map[string]any{
		"conversationId": threadID, "threadSettings": map[string]any{"model": v.Model, "effort": effort, "serviceTier": tier}, "condition": map[string]any{"ifModelEquals": old.Model, "ifEffortEquals": identity.Effort},
	}})
	if err != nil {
		return err
	}
	var result struct {
		Applied *bool `json:"applied"`
	}
	if json.Unmarshal(r.Result, &result) != nil || result.Applied == nil {
		return &CallError{Cause: ErrProtocol, Outcome: "unknown"}
	}
	if !*result.Applied {
		return modelsettings.ErrConflict
	}
	return nil
}

func (f *Follower) ExecutionSettings() (modelsettings.Values, error) {
	f.stateMu.Lock()
	defer f.stateMu.Unlock()
	if f.err != nil {
		return modelsettings.Values{}, f.err
	}
	return ExecutionSettings(f.o.reducer.ExecutionMetadata())
}
func (f *Follower) UpdateExecutionSettings(ctx context.Context, v modelsettings.Values) error {
	f.startMu.Lock()
	defer f.startMu.Unlock()
	raw, err := f.Current()
	if err != nil {
		return err
	}
	if err = UpdateExecutionSettings(ctx, f.c, f.o.owner, f.o.threadID, f.cwd, raw, v); err != nil {
		return err
	}
	// Only follower patches can confirm the write. Never call load-complete-history.
	for {
		got, err := f.ExecutionSettings()
		if err != nil {
			return &CallError{Cause: err, Outcome: "unknown"}
		}
		if got == v {
			return nil
		}
		select {
		case <-f.wake:
		case <-ctx.Done():
			return &CallError{Cause: ctx.Err(), Outcome: "unknown"}
		case <-f.Done():
			return &CallError{Cause: ErrResyncRequired, Outcome: "unknown"}
		}
	}
}
