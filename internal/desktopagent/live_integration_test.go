package desktopagent

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/wu8685/Ariel/internal/codex/appserver"
	"github.com/wu8685/Ariel/internal/codex/desktopipc"
	"github.com/wu8685/Ariel/internal/probe"
)

type singleFixtureHistory struct{ thread appserver.Thread }

func (h singleFixtureHistory) List(context.Context, string, int) (appserver.Page, error) {
	return appserver.Page{Data: []appserver.Thread{h.thread}}, nil
}
func (h singleFixtureHistory) Read(context.Context, string) (appserver.Thread, error) {
	return h.thread, nil
}

func fixtureMessageID(t *testing.T) string {
	t.Helper()
	var bits [16]byte
	if _, err := rand.Read(bits[:]); err != nil {
		t.Fatal(err)
	}
	bits[6] = (bits[6] & 0x0f) | 0x40
	bits[8] = (bits[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", bits[:4], bits[4:6], bits[6:8], bits[8:10], bits[10:])
}

// Opt-in real-owner race against a separate IPC client. Only a verified,
// no-tools fixture may receive these prompts; business threads are excluded.
func TestRealIndependentClientConcurrentStart(t *testing.T) {
	path := os.Getenv("ARIEL_TEST_MANIFEST")
	if path == "" || os.Getenv("ARIEL_TEST_WRITE_ENABLED") != "1" || os.Getenv("ARIEL_TEST_CONCURRENT") != "1" {
		t.Skip("requires an isolated no-tools fixture and explicit concurrent write enable")
	}
	manifest, err := probe.LoadManifest(path)
	if err != nil {
		t.Fatal("fixture manifest unavailable or malformed")
	}
	if manifest.Purpose != "" || manifest.Authorize(true, manifest.ThreadID, manifest.Workspace) != nil {
		t.Fatal("not a verified no-tools fixture")
	}
	cfg, err := probe.Defaults()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	first, err := OpenFollower(ctx, cfg.Socket, manifest.ThreadID, manifest.Workspace)
	if err != nil {
		t.Fatal(err)
	}
	second, err := OpenFollower(ctx, cfg.Socket, manifest.ThreadID, manifest.Workspace)
	if err != nil {
		first.Close()
		t.Fatal(err)
	}
	defer second.Close()
	service := NewService(singleFixtureHistory{thread: appserver.Thread{ID: manifest.ThreadID, Name: "isolated fixture", CWD: manifest.Workspace}}, func(context.Context, string, string) (Live, error) { return first, nil })
	defer service.Close()

	state, err := second.(*desktopipc.Follower).Refresh(ctx)
	if err != nil {
		t.Fatal(err)
	}
	before, err := NormalizeLive(manifest.ThreadID, "isolated fixture", manifest.Workspace, state)
	if err != nil || before["runtime"] != "idle" || len(before["pendingInteractions"].([]any)) != 0 {
		t.Fatal("fixture is not idle before concurrent start")
	}
	defer cleanupConcurrentFixture(t, second.(*desktopipc.Follower), manifest.ThreadID, manifest.Workspace)
	idA, idB := fixtureMessageID(t), fixtureMessageID(t)
	promptA := "Ariel isolated concurrent start A. Do not use tools or inspect files. Output the integers 1 through 5000, one per line."
	promptB := "Ariel isolated concurrent start B. Do not use tools or inspect files. Output the integers 5001 through 10000, one per line."
	type result struct {
		side, id, prompt, turn string
		err                    error
	}
	gate := make(chan struct{})
	results := make(chan result, 2)
	go func() {
		<-gate
		turn, err := service.Start(ctx, manifest.ThreadID, idA, promptA)
		results <- result{side: "Agent", id: idA, prompt: promptA, turn: turn, err: err}
	}()
	go func() {
		<-gate
		turn, err := second.Start(ctx, idB, promptB)
		results <- result{side: "independent", id: idB, prompt: promptB, turn: turn, err: err}
	}()
	close(gate)
	var outcomes []result
	for range 2 {
		select {
		case item := <-results:
			outcomes = append(outcomes, item)
		case <-ctx.Done():
			t.Fatal("concurrent fixture starts did not settle")
		}
	}
	state, err = refreshFixtureState(ctx, second.(*desktopipc.Follower))
	if err != nil {
		t.Fatal(err)
	}
	observed, normalizeErr := NormalizeLive(manifest.ThreadID, "isolated fixture", manifest.Workspace, state)
	turnIDs := make([]string, 0)
	if normalizeErr == nil {
		for _, rawTurn := range observed["turns"].([]any) {
			turnIDs = append(turnIDs, rawTurn.(map[string]any)["turnId"].(string))
		}
	} else {
		for id := range nativeFixtureTurns(state) {
			if id != "" {
				turnIDs = append(turnIDs, id)
			}
		}
	}
	for _, item := range outcomes {
		category := "error"
		if item.err == nil {
			category = "accepted"
		}
		if errors.Is(item.err, desktopipc.ErrTurnBusy) || (item.err != nil && item.err.Error() == "TURN_BUSY") {
			category = "busy"
		}
		var call *desktopipc.CallError
		if errors.As(item.err, &call) && call.Outcome == "unknown" {
			category = "unknown"
		}
		matches := 0
		for _, turnID := range turnIDs {
			if desktopipc.TurnContainsClientMessage(state, turnID, item.id, item.prompt) {
				matches++
			}
		}
		if item.err == nil && matches != 1 {
			t.Fatalf("%s accepted without exact native client-message identity", item.side)
		}
		t.Logf("%s outcome=%s nativeIdentityMatches=%d", item.side, category, matches)
	}
	if normalizeErr != nil {
		t.Errorf("concurrent owner state cannot be normalized; shape=%v", fixtureNativeShape(state, manifest.Workspace))
	}
}

func cleanupConcurrentFixture(t *testing.T, follower *desktopipc.Follower, threadID, cwd string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	idleSince := time.Time{}
	lastRuntime, lastActive, attempts, accepted := "unknown", 0, 0, 0
	attempted := map[string]int{}
	for ctx.Err() == nil {
		state, err := refreshFixtureState(ctx, follower)
		if err != nil {
			t.Error("fixture cleanup refresh failed")
			return
		}
		var native struct {
			CWD     string `json:"cwd"`
			Runtime struct {
				Type string `json:"type"`
			} `json:"threadRuntimeStatus"`
		}
		if json.Unmarshal(state, &native) != nil || native.CWD != cwd {
			t.Error("fixture cleanup identity could not be verified")
			return
		}
		active := map[string]struct{}{}
		lastRuntime = native.Runtime.Type
		for id, status := range nativeFixtureTurns(state) {
			if id != "" && status == "inProgress" {
				active[id] = struct{}{}
			}
		}
		lastActive = len(active)
		if len(active) == 0 && lastRuntime == "idle" {
			if idleSince.IsZero() {
				idleSince = time.Now()
			}
			if time.Since(idleSince) >= time.Second {
				return
			}
		} else {
			idleSince = time.Time{}
			for id := range active {
				if attempted[id] >= 2 {
					continue
				}
				attempted[id]++
				attempts++
				if follower.Interrupt(ctx, id) == nil {
					accepted++
				}
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Errorf("fixture did not settle after exact-turn cleanup: runtime=%s active=%d interrupts=%d accepted=%d", lastRuntime, lastActive, attempts, accepted)
}

func refreshFixtureState(ctx context.Context, follower *desktopipc.Follower) (json.RawMessage, error) {
	state, err := follower.Refresh(ctx)
	if errors.Is(err, desktopipc.ErrNativeStateUncertain) {
		// The product correctly refuses mutation, but this diagnostic still
		// needs the raw snapshot to identify and stop only addressable turns.
		return follower.Current()
	}
	return state, err
}

// Read only IDs and statuses for exact-turn cleanup. A native empty-ID ghost
// cannot be interrupted and must never become an unscoped stop request.
func nativeFixtureTurns(state json.RawMessage) map[string]string {
	var native struct {
		Turns       []struct{ TurnID, Status string } `json:"turns"`
		TurnHistory struct {
			History struct {
				Entities map[string]struct{ TurnID, Status string } `json:"entitiesByKey"`
			} `json:"history"`
		} `json:"turnHistory"`
	}
	if json.Unmarshal(state, &native) != nil {
		return nil
	}
	out := map[string]string{}
	for _, turn := range native.Turns {
		out[turn.TurnID] = turn.Status
	}
	for _, turn := range native.TurnHistory.History.Entities {
		out[turn.TurnID] = turn.Status
	}
	return out
}

// Opt-in mutation restricted to a verified no-tools fixture. It determines
// whether native userMessage.id really equals clientUserMessageId.
func TestRealClientMessageIdentity(t *testing.T) {
	path := os.Getenv("ARIEL_TEST_MANIFEST")
	if path == "" || os.Getenv("ARIEL_TEST_WRITE_ENABLED") != "1" {
		t.Skip("requires isolated fixture manifest and explicit write enable")
	}
	manifest, err := probe.LoadManifest(path)
	if err != nil || manifest.Purpose != "" || manifest.Authorize(true, manifest.ThreadID, manifest.Workspace) != nil {
		t.Skip("not a verified no-tools fixture")
	}
	cfg, err := probe.Defaults()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	live, err := OpenFollower(ctx, cfg.Socket, manifest.ThreadID, manifest.Workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer live.Close()
	var bits [16]byte
	if _, err := rand.Read(bits[:]); err != nil {
		t.Fatal(err)
	}
	bits[6] = (bits[6] & 0x0f) | 0x40
	bits[8] = (bits[8] & 0x3f) | 0x80
	messageID := fmt.Sprintf("%x-%x-%x-%x-%x", bits[:4], bits[4:6], bits[6:8], bits[8:10], bits[10:])
	prompt := "Ariel isolated client-message identity check. Do not use tools, inspect files, or create tasks. Reply exactly ARIEL_MESSAGE_ID_OK."
	turnID, err := live.Start(ctx, messageID, prompt)
	if err != nil {
		t.Fatal(err)
	}
	for ctx.Err() == nil {
		state, err := live.(*desktopipc.Follower).Refresh(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var snapshot struct {
			TurnHistory struct {
				History struct {
					Entities map[string]struct {
						TurnID string            `json:"turnId"`
						Items  []json.RawMessage `json:"items"`
					} `json:"entitiesByKey"`
				} `json:"history"`
			} `json:"turnHistory"`
		}
		if json.Unmarshal(state, &snapshot) != nil {
			t.Fatal("invalid fixture snapshot")
		}
		for _, turn := range snapshot.TurnHistory.History.Entities {
			if turn.TurnID != turnID {
				continue
			}
			for _, item := range turn.Items {
				var user struct {
					ID       string `json:"id"`
					ClientID string `json:"clientId"`
					Type     string `json:"type"`
					Content  []struct {
						Type string `json:"type"`
						Text string `json:"text"`
					} `json:"content"`
				}
				if json.Unmarshal(item, &user) != nil || user.Type != "userMessage" {
					continue
				}
				if len(user.Content) == 0 || user.Content[0].Text != prompt {
					t.Fatal("wrong user message attached to accepted turn")
				}
				if user.ClientID != messageID {
					t.Fatal("native userMessage.clientId does not match clientUserMessageId")
				}
				return
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatal("fixture turn identity not observed")
}

// Opt-in integration diagnostic. Only reads an isolated fixture named by env.
func TestRealFollowerRepeatedRefresh(t *testing.T) {
	path := os.Getenv("ARIEL_TEST_MANIFEST")
	if path == "" {
		t.Skip("set ARIEL_TEST_MANIFEST for isolated Desktop fixture")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		ThreadID  string `json:"threadId"`
		Workspace string `json:"workspace"`
	}
	if json.Unmarshal(body, &manifest) != nil || manifest.ThreadID == "" || manifest.Workspace == "" {
		t.Fatal("invalid fixture manifest")
	}
	cfg, err := probe.Defaults()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	live, err := OpenFollower(ctx, cfg.Socket, manifest.ThreadID, manifest.Workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer live.Close()
	f := live.(*desktopipc.Follower)
	for i := 0; i < 3; i++ {
		started := time.Now()
		state, err := f.Refresh(ctx)
		if err != nil {
			t.Fatalf("refresh %d after %s: %v", i, time.Since(started), err)
		}
		thread, err := NormalizeLive(manifest.ThreadID, "fixture", manifest.Workspace, state)
		if err != nil {
			t.Fatalf("%v; native shape: %v", err, fixtureNativeShape(state, manifest.Workspace))
		}
		if i == 0 {
			var settings struct {
				LatestThreadSettings struct {
					ApprovalPolicy string `json:"approvalPolicy"`
					SandboxPolicy  struct {
						Type string `json:"type"`
					} `json:"sandboxPolicy"`
				} `json:"latestThreadSettings"`
			}
			_ = json.Unmarshal(state, &settings)
			t.Logf("fixture approvalPolicy=%s sandboxPolicy=%s", settings.LatestThreadSettings.ApprovalPolicy, settings.LatestThreadSettings.SandboxPolicy.Type)
		}
		statuses := make([]string, 0)
		for _, turn := range thread["turns"].([]any) {
			statuses = append(statuses, turn.(map[string]any)["status"].(string))
		}
		t.Logf("refresh %d duration=%s runtime=%s turnStatuses=%v", i, time.Since(started), thread["runtime"], statuses)
	}
}

func fixtureNativeShape(state json.RawMessage, expectedCWD string) map[string]any {
	var native struct {
		CWD                 string `json:"cwd"`
		ThreadRuntimeStatus struct {
			Type string `json:"type"`
		} `json:"threadRuntimeStatus"`
		Requests    []json.RawMessage `json:"requests"`
		TurnHistory struct {
			Kind    string `json:"kind"`
			History struct {
				Islands []struct {
					Entries []struct {
						Value string `json:"value"`
					} `json:"entries"`
				} `json:"islands"`
				Entities map[string]struct {
					TurnID string `json:"turnId"`
					Status string `json:"status"`
					Items  []struct {
						ID, Type string
						Content  []struct {
							Text string `json:"text"`
						} `json:"content"`
					} `json:"items"`
				} `json:"entitiesByKey"`
			} `json:"history"`
		} `json:"turnHistory"`
	}
	if json.Unmarshal(state, &native) != nil {
		return map[string]any{"jsonValid": false}
	}
	statuses, itemTypes := map[string]int{}, map[string]int{}
	missingIDs, missingTurnIDs, entries, missingEntries := 0, 0, 0, 0
	ghostMarkers := map[string]int{}
	ghostItemCounts := map[string]int{}
	for _, island := range native.TurnHistory.History.Islands {
		for _, entry := range island.Entries {
			entries++
			if _, ok := native.TurnHistory.History.Entities[entry.Value]; !ok {
				missingEntries++
			}
		}
	}
	for _, turn := range native.TurnHistory.History.Entities {
		if turn.TurnID == "" {
			missingTurnIDs++
			ghostItemCounts[turn.Status] += len(turn.Items)
			for _, item := range turn.Items {
				for _, part := range item.Content {
					if strings.HasPrefix(part.Text, "Ariel isolated concurrent start A.") {
						ghostMarkers["A"]++
					}
					if strings.HasPrefix(part.Text, "Ariel isolated concurrent start B.") {
						ghostMarkers["B"]++
					}
				}
			}
		}
		statuses[turn.Status]++
		for _, item := range turn.Items {
			itemTypes[item.Type]++
			if item.ID == "" {
				missingIDs++
			}
		}
	}
	return map[string]any{"cwdMatches": native.CWD == expectedCWD, "runtime": native.ThreadRuntimeStatus.Type, "requests": len(native.Requests), "historyKind": native.TurnHistory.Kind, "entities": len(native.TurnHistory.History.Entities), "entries": entries, "missingEntries": missingEntries, "statuses": statuses, "itemTypes": itemTypes, "missingItemIDs": missingIDs, "missingTurnIDs": missingTurnIDs, "ghostMarkers": ghostMarkers, "ghostItemCounts": ghostItemCounts}
}

// Opt-in fixture-only shape capture; never point this at a business thread.
func TestRealFileApprovalShape(t *testing.T) {
	path := os.Getenv("ARIEL_TEST_MANIFEST")
	if path == "" {
		t.Skip("set ARIEL_TEST_MANIFEST for isolated file-change fixture")
	}
	manifest, err := probe.LoadManifest(path)
	if err != nil || manifest.Purpose != "file-change" || manifest.Authorize(true, manifest.ThreadID, manifest.Workspace) != nil {
		t.Skip("not a verified file-change fixture")
	}
	cfg, err := probe.Defaults()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	live, err := OpenFollower(ctx, cfg.Socket, manifest.ThreadID, manifest.Workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer live.Close()
	state, err := live.(*desktopipc.Follower).Refresh(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var snapshot struct {
		Requests    []json.RawMessage `json:"requests"`
		TurnHistory struct {
			History struct {
				Entities map[string]struct {
					Items []json.RawMessage `json:"items"`
				} `json:"entitiesByKey"`
			} `json:"history"`
		} `json:"turnHistory"`
	}
	if json.Unmarshal(state, &snapshot) != nil || len(snapshot.Requests) != 1 {
		t.Fatal("expected one pending isolated file request")
	}
	t.Logf("fixture request=%s", snapshot.Requests[0])
	for _, turn := range snapshot.TurnHistory.History.Entities {
		for _, item := range turn.Items {
			var header struct {
				Type string `json:"type"`
			}
			if json.Unmarshal(item, &header) == nil && header.Type == "fileChange" {
				t.Logf("fixture fileChange item=%s", item)
			}
		}
	}
}
