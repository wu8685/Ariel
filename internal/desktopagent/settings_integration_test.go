package desktopagent

import (
	"context"
	"encoding/json"
	"github.com/wu8685/Ariel/internal/codex/appserver"
	"github.com/wu8685/Ariel/internal/codex/desktopipc"
	"github.com/wu8685/Ariel/internal/modelsettings"
	"github.com/wu8685/Ariel/internal/probe"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// This gate creates its own guarded, no-tools fixture. Never accept a caller's
// thread ID: every mutation and inference stays inside the freshly made fixture.
func TestRealConversationExecutionSettings(t *testing.T) {
	if os.Getenv("ARIEL_TEST_SETTINGS_WRITE") != "1" {
		t.Skip("requires explicit isolated settings write gate")
	}
	cfg, err := probe.Defaults()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	process, err := appserver.StartInteractive(ctx, probe.BundledBinary(cfg.AppPath), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer process.Close()
	catalog, err := (appserver.ModelReader{RPC: process}).Models(ctx)
	if err != nil || len(catalog.Models) == 0 {
		t.Fatal("catalog unavailable", err)
	}
	model := catalog.Models[0]
	if len(model.Efforts) < 2 || len(model.Tiers) == 0 {
		t.Fatal("fixture needs advertised reasoning and speed choices")
	}
	parent := t.TempDir()
	m, err := probe.CreateFixture(ctx, process, probe.CreateOptions{Parent: parent, ManifestPath: filepath.Join(parent, "fixture.json"), WriteEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if err = m.Authorize(true, m.ThreadID, m.Workspace); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		ep, e := appserver.Start(cleanup, probe.BundledBinary(cfg.AppPath), m.Workspace)
		if e == nil {
			defer ep.Close()
			_ = (appserver.ThreadArchiver{RPC: ep}).Archive(cleanup, m.ThreadID)
		}
	}()
	var turn struct {
		Turn struct {
			ID string `json:"id"`
		} `json:"turn"`
	}
	if err = process.Call(ctx, "turn/start", map[string]any{"threadId": m.ThreadID, "model": model.ID, "effort": model.Efforts[0].ID, "input": []any{map[string]any{"type": "text", "text": "Reply exactly ARIEL_SETTINGS_SEED. Do not use tools.", "text_elements": []any{}}}}, &turn); err != nil {
		t.Fatal(err)
	}
	seeded := false
	for !seeded {
		select {
		case frame := <-process.Incoming():
			if frame.IsRequest() {
				t.Fatal("unexpected tool request in no-tools fixture")
			}
			if frame.Method == "turn/completed" {
				seeded = true
			}
		case <-ctx.Done():
			t.Fatal("seed timeout")
		}
	}
	_ = process.Close()
	// Select a different advertised model, not just a different effort on the seed model.
	model = catalog.Models[len(catalog.Models)-1]
	if len(model.Efforts) < 2 || len(model.Tiers) == 0 {
		t.Fatal("second fixture model has no comparable choices")
	}
	live, err := OpenFollower(ctx, cfg.Socket, m.ThreadID, m.Workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer live.Close()
	follower, ok := live.(*desktopipc.Follower)
	if !ok {
		t.Fatal("unexpected fixture wrapper")
	}
	queueProcess, err := appserver.Start(ctx, probe.BundledBinary(cfg.AppPath), m.Workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer queueProcess.Close()
	queue := appserver.QueueReader{RPC: queueProcess.Session}
	for _, speed := range []modelsettings.Speed{{Mode: "tier", TierID: model.Tiers[0].ID}, {Mode: "standard"}, {Mode: "inherit"}} {
		desired := modelsettings.Values{Model: model.ID, Effort: model.Efforts[1].ID, Speed: speed}
		change, stop := context.WithTimeout(ctx, 10*time.Second)
		err = follower.UpdateExecutionSettings(change, desired)
		stop()
		if err != nil {
			t.Fatalf("speed %s write/readback: %v", speed.Mode, err)
		}
		got, err := follower.ExecutionSettings()
		if err != nil || got != desired {
			t.Fatalf("speed %s not reflected", speed.Mode)
		}
		started, err := follower.Start(ctx, fixtureMessageID(t), "Reply exactly ARIEL_SETTINGS_OK. Do not use tools.")
		if err != nil {
			t.Fatal("settings start", err)
		}
		terminal := false
		changedWhileRunning := false
		queuedTerminal := speed.Mode != "tier"
		queuedMessageID := ""
		queuedText := "Reply exactly ARIEL_QUEUED_SETTINGS_OK. Do not use tools."
		for !terminal || !queuedTerminal {
			raw, e := follower.Current()
			if e != nil {
				t.Fatal(e)
			}
			var state struct {
				History struct {
					History struct {
						Entities map[string]struct {
							TurnID string `json:"turnId"`
							Status string `json:"status"`
							Params struct {
								Model  string  `json:"model"`
								Effort string  `json:"effort"`
								Tier   *string `json:"serviceTier"`
								Mode   *struct {
									Settings struct {
										Model  string `json:"model"`
										Effort string `json:"reasoning_effort"`
									} `json:"settings"`
								} `json:"collaborationMode"`
							} `json:"params"`
						} `json:"entitiesByKey"`
					} `json:"history"`
				} `json:"turnHistory"`
			}
			if json.Unmarshal(raw, &state) != nil {
				t.Fatal("state shape")
			}
			for _, turn := range state.History.History.Entities {
				if turn.TurnID != started {
					if queuedMessageID != "" && desktopipc.TurnContainsClientMessage(raw, turn.TurnID, queuedMessageID, queuedText) {
						actualModel, effort := turn.Params.Model, turn.Params.Effort
						if turn.Params.Mode != nil {
							actualModel = turn.Params.Mode.Settings.Model
							effort = turn.Params.Mode.Settings.Effort
						}
						if actualModel != model.ID || effort != model.Efforts[0].ID || turn.Params.Tier == nil || *turn.Params.Tier != speed.TierID {
							t.Fatal("queued turn ignored next-turn settings")
						}
						queuedTerminal = turn.Status == "completed"
					}
					continue
				}
				actualModel, effort := turn.Params.Model, turn.Params.Effort
				if turn.Params.Mode != nil {
					actualModel = turn.Params.Mode.Settings.Model
					effort = turn.Params.Mode.Settings.Effort
				}
				if actualModel != desired.Model || effort != desired.Effort {
					t.Fatal("next turn did not use chosen model/effort")
				}
				if speed.Mode == "tier" && (turn.Params.Tier == nil || *turn.Params.Tier != speed.TierID) {
					t.Fatal("tier ignored")
				}
				if speed.Mode == "standard" && (turn.Params.Tier == nil || *turn.Params.Tier != "default") {
					t.Fatal("standard ignored")
				}
				terminal = turn.Status == "completed"
				if speed.Mode == "tier" && turn.Status == "inProgress" && !changedWhileRunning {
					next := desired
					next.Effort = model.Efforts[0].ID
					change, stop := context.WithTimeout(ctx, 10*time.Second)
					err := follower.UpdateExecutionSettings(change, next)
					stop()
					if err != nil {
						t.Fatal("running next-turn settings", err)
					}
					changedWhileRunning = true
					queuedMessageID = fixtureMessageID(t)
					queued, e := queue.Add(ctx, m.ThreadID, queuedMessageID, queuedText, nil)
					if e != nil || queued.ID == "" {
						t.Fatal("settings queued follow-up", e)
					}
					defer func() {
						cleanup, stop := context.WithTimeout(context.Background(), 3*time.Second)
						defer stop()
						_, _ = queue.Delete(cleanup, m.ThreadID, queued.ID)
					}()
				}
			}
			if !terminal || !queuedTerminal {
				select {
				case <-follower.Updates():
				case <-ctx.Done():
					t.Fatal("settings turn completion timeout")
				}
			}
		}
		if speed.Mode == "tier" && !changedWhileRunning {
			t.Fatal("fixture did not exercise running-turn update")
		}
		t.Logf("verified native write, follower readback and next turn: %s", speed.Mode)
	}
	_ = live.Close()
	reopened, e := OpenFollower(ctx, cfg.Socket, m.ThreadID, m.Workspace)
	if e != nil {
		t.Fatal("reopen fixture", e)
	}
	defer reopened.Close()
	got, e := reopened.(executionSettingsLive).ExecutionSettings()
	if e != nil || got.Model != model.ID || got.Effort != model.Efforts[1].ID || got.Speed.Mode != "inherit" {
		t.Fatal("reopened settings not retained")
	}
	verifyNativeSettingsWire(t, ctx, m, reopened, queueProcess, model.Efforts[0].ID)
}

func TestRealBootstrapExecutionSettings(t *testing.T) {
	if os.Getenv("ARIEL_TEST_SETTINGS_WRITE") != "1" {
		t.Skip("explicit isolated settings gate")
	}
	cfg, err := probe.Defaults()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cwd, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	reader, err := appserver.Start(ctx, probe.BundledBinary(cfg.AppPath), cwd)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	catalog, err := (appserver.ModelReader{RPC: reader}).Models(ctx)
	if err != nil || len(catalog.Models) == 0 {
		t.Fatal("catalog", err)
	}
	m := catalog.Models[len(catalog.Models)-1]
	desired := modelsettings.Values{Model: m.ID, Effort: m.DefaultEffort, Speed: modelsettings.Speed{Mode: "standard"}}
	service := NewService(appserver.HistoryReader{RPC: reader}, func(ctx context.Context, id, cwd string) (Live, error) { return OpenFollower(ctx, cfg.Socket, id, cwd) })
	defer service.Close()
	service.SetModelReader(appserver.ModelReader{RPC: reader})
	service.SetThreadBootstrapper(AppServerBootstrapper{Binary: probe.BundledBinary(cfg.AppPath)})
	thread, _, err := service.BootstrapThreadWithSettings(ctx, cwd, fixtureMessageID(t), "Reply exactly ARIEL_BOOTSTRAP_SETTINGS_OK. Do not use tools.", nil, &desired)
	if err != nil {
		t.Fatal("explicit bootstrap settings", err)
	}
	id := thread["threadId"].(string)
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		_ = (appserver.ThreadArchiver{RPC: reader}).Archive(cleanup, id)
	}()
	events := make(chan struct{}, 1)
	_, _, _, activate, err := service.Subscribe(ctx, id, func(map[string]any) {
		select {
		case events <- struct{}{}:
		default:
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	activate()
	initial, err := service.ReadSettings(ctx, id)
	if err != nil || initial.Values != desired {
		t.Fatal("bootstrap setting confirmation differs", err)
	}
	for {
		adopted, err := service.ReadSettings(ctx, id)
		if err == nil && adopted.Writable {
			if adopted.Values != desired {
				t.Fatalf("bootstrap settings lost during Desktop adoption: got=%+v", adopted.Values)
			}
			break
		}
		select {
		case <-events:
		case <-ctx.Done():
			t.Fatal("bootstrap Desktop adoption timeout")
		}
	}
	t.Log("explicit bootstrap model, effort, standard speed, completion and Desktop adoption verified")
}
