package desktopagent

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/wu8685/Ariel/internal/codex/appserver"
	"github.com/wu8685/Ariel/internal/codex/desktopipc"
	"github.com/wu8685/Ariel/internal/probe"
)

type singleFixtureHistory struct{ thread appserver.Thread }

type droppedStartReceipt struct {
	Live
	acceptedTurn string
	calls        int
}

type droppedApprovalReceipt struct {
	Live
	calls int
}

func (l *droppedStartReceipt) Start(ctx context.Context, messageID, text string) (string, error) {
	l.calls++
	turnID, err := l.Live.Start(ctx, messageID, text)
	if err != nil {
		return turnID, err
	}
	l.acceptedTurn = turnID
	return "", &desktopipc.CallError{Cause: errors.New("injected lost start receipt"), Outcome: "unknown"}
}

func (l *droppedApprovalReceipt) Respond(ctx context.Context, interactionID, decision string, answers map[string][]string) error {
	l.calls++
	if err := l.Live.Respond(ctx, interactionID, decision, answers); err != nil {
		return err
	}
	return &desktopipc.CallError{Cause: errors.New("injected lost approval receipt"), Outcome: "unknown"}
}

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

// Opt-in real-owner fault injection. The owner receives and verifies the
// message; only the result delivered back to Agent Service is discarded.
func TestRealAcceptedStartWithLostReceiptIsNotReplayed(t *testing.T) {
	path := os.Getenv("ARIEL_TEST_MANIFEST")
	if path == "" || os.Getenv("ARIEL_TEST_WRITE_ENABLED") != "1" || os.Getenv("ARIEL_TEST_LOST_RECEIPT") != "1" {
		t.Skip("requires a fresh no-tools fixture and explicit lost-receipt fault injection")
	}
	manifest, err := probe.LoadManifest(path)
	if err != nil || manifest.Purpose != "" || manifest.Authorize(true, manifest.ThreadID, manifest.Workspace) != nil {
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
	defer cleanupConcurrentFixture(t, second.(*desktopipc.Follower), manifest.ThreadID, manifest.Workspace)
	live := &droppedStartReceipt{Live: first}
	service := NewService(singleFixtureHistory{thread: appserver.Thread{ID: manifest.ThreadID, Name: "isolated fixture", CWD: manifest.Workspace}}, func(context.Context, string, string) (Live, error) { return live, nil })
	defer service.Close()

	state, err := second.(*desktopipc.Follower).Refresh(ctx)
	if err != nil {
		t.Fatal(err)
	}
	before, err := NormalizeLive(manifest.ThreadID, "isolated fixture", manifest.Workspace, state)
	if err != nil || before["runtime"] != "idle" || len(before["pendingInteractions"].([]any)) != 0 {
		t.Fatal("fixture is not idle before lost-receipt test")
	}
	messageID := fixtureMessageID(t)
	prompt := "Ariel isolated lost-receipt check. Do not use tools or inspect files. Reply exactly ARIEL_A15_OK."
	_, err = service.Start(ctx, manifest.ThreadID, messageID, prompt)
	var callErr *desktopipc.CallError
	if !errors.As(err, &callErr) || callErr.Outcome != "unknown" || live.acceptedTurn == "" || live.calls != 1 {
		t.Fatalf("accepted owner start was not reported unknown after receipt drop: err=%v calls=%d", err, live.calls)
	}
	if _, err := service.Start(ctx, manifest.ThreadID, messageID, prompt); err == nil || err.Error() != "INVALID_ARGUMENT" || live.calls != 1 {
		t.Fatalf("same message ID was replayed after unknown: err=%v calls=%d", err, live.calls)
	}
	for ctx.Err() == nil {
		state, err = refreshFixtureState(ctx, second.(*desktopipc.Follower))
		if err != nil {
			t.Fatal(err)
		}
		if desktopipc.TurnContainsClientMessage(state, live.acceptedTurn, messageID, prompt) {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatal("original Desktop owner never showed the accepted message identity")
}

// Opt-in real-owner approval fault injection. Only a verified fixture with
// a pending /usr/bin/true request may be denied; the command is never allowed.
func TestRealDeniedApprovalWithLostReceiptIsNotReplayed(t *testing.T) {
	path := os.Getenv("ARIEL_TEST_MANIFEST")
	if path == "" || os.Getenv("ARIEL_TEST_WRITE_ENABLED") != "1" || os.Getenv("ARIEL_TEST_LOST_APPROVAL") != "1" {
		t.Skip("requires a pending command fixture and explicit lost-approval fault injection")
	}
	manifest, err := probe.LoadManifest(path)
	if err != nil || manifest.Purpose != "command-accept" || manifest.Authorize(true, manifest.ThreadID, manifest.Workspace) != nil {
		t.Fatal("not a verified command-approval fixture")
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
	defer cleanupConcurrentFixture(t, second.(*desktopipc.Follower), manifest.ThreadID, manifest.Workspace)
	live := &droppedApprovalReceipt{Live: first}
	service := NewService(singleFixtureHistory{thread: appserver.Thread{ID: manifest.ThreadID, Name: "isolated fixture", CWD: manifest.Workspace}}, func(context.Context, string, string) (Live, error) { return live, nil })
	defer service.Close()

	state, err := second.(*desktopipc.Follower).Refresh(ctx)
	if err != nil {
		t.Fatal(err)
	}
	before, err := NormalizeLive(manifest.ThreadID, "isolated fixture", manifest.Workspace, state)
	if err != nil {
		t.Fatal(err)
	}
	cards := before["pendingInteractions"].([]any)
	if len(cards) != 1 {
		t.Fatalf("expected exactly one pending approval, got %d", len(cards))
	}
	card := cards[0].(map[string]any)
	prompt, _ := card["prompt"].(string)
	if card["kind"] != "command_approval" || !strings.Contains(prompt, "/usr/bin/true") {
		t.Fatal("fixture does not contain the exact harmless command approval")
	}
	decisions := card["availableDecisions"].([]string)
	decision := ""
	for _, offered := range decisions {
		if offered == "deny" || (offered == "deny_and_stop" && decision == "") {
			decision = offered
		}
	}
	if decision == "" {
		t.Fatal("native request did not offer a safe denial")
	}
	interactionID := card["interactionId"].(string)
	err = service.Respond(ctx, manifest.ThreadID, interactionID, decision, nil)
	var callErr *desktopipc.CallError
	if !errors.As(err, &callErr) || callErr.Outcome != "unknown" || live.calls != 1 {
		t.Fatalf("owner-confirmed denial was not reported unknown after receipt drop: err=%v calls=%d", err, live.calls)
	}
	for ctx.Err() == nil {
		state, err = refreshFixtureState(ctx, second.(*desktopipc.Follower))
		if err != nil {
			t.Fatal(err)
		}
		after, normalizeErr := NormalizeLive(manifest.ThreadID, "isolated fixture", manifest.Workspace, state)
		if normalizeErr == nil && len(after["pendingInteractions"].([]any)) == 0 {
			if live.calls != 1 {
				t.Fatal("approval was replayed after unknown result")
			}
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatal("original Desktop owner kept the approval pending after confirmed denial")
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

// Read-only diagnostic: determine whether the public App Server history
// exposes permission settings needed to compare against Desktop's live owner.
func TestRealFixtureStoredSettingsVisibility(t *testing.T) {
	path := os.Getenv("ARIEL_TEST_MANIFEST")
	if path == "" {
		t.Skip("set ARIEL_TEST_MANIFEST for isolated Desktop fixture")
	}
	manifest, err := probe.LoadManifest(path)
	if err != nil || manifest.Authorize(true, manifest.ThreadID, manifest.Workspace) != nil {
		t.Fatal("not a verified isolated fixture")
	}
	cfg, err := probe.Defaults()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	rpc, err := appserver.Start(ctx, probe.BundledBinary(cfg.AppPath), os.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer rpc.Close()
	var result struct {
		Thread map[string]json.RawMessage `json:"thread"`
	}
	if err := rpc.Call(ctx, "thread/read", map[string]any{"threadId": manifest.ThreadID, "includeTurns": true}, &result); err != nil {
		t.Fatal(err)
	}
	keys := make([]string, 0, len(result.Thread))
	for key := range result.Thread {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var turns []map[string]json.RawMessage
	if err := json.Unmarshal(result.Thread["turns"], &turns); err != nil {
		t.Fatal("history turns unavailable")
	}
	turnKeys := []string{}
	if len(turns) > 0 {
		for key := range turns[len(turns)-1] {
			turnKeys = append(turnKeys, key)
		}
		sort.Strings(turnKeys)
	}
	t.Logf("App Server thread fields=%v last turn fields=%v", keys, turnKeys)
	live, err := OpenFollower(ctx, cfg.Socket, manifest.ThreadID, manifest.Workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer live.Close()
	state, err := live.Current()
	if err != nil {
		t.Fatal(err)
	}
	var owner struct {
		LatestThreadSettings struct {
			ApprovalPolicy string `json:"approvalPolicy"`
			SandboxPolicy  struct {
				Type string `json:"type"`
			} `json:"sandboxPolicy"`
		} `json:"latestThreadSettings"`
	}
	if json.Unmarshal(state, &owner) != nil {
		t.Fatal("owner settings unavailable")
	}
	t.Logf("owner approvalPolicy=%s sandboxPolicy=%s", owner.LatestThreadSettings.ApprovalPolicy, owner.LatestThreadSettings.SandboxPolicy.Type)
}

// Opt-in, read-only persistence check after a Web send. It never prints chat
// contents or sends a turn; the caller supplies a unique fixture-only marker.
func TestRealFixturePersistedNoToolExchange(t *testing.T) {
	path, marker := os.Getenv("ARIEL_TEST_MANIFEST"), os.Getenv("ARIEL_TEST_EXPECT_MARKER")
	if path == "" || marker == "" {
		t.Skip("set an isolated fixture manifest and expected marker")
	}
	manifest, err := probe.LoadManifest(path)
	if err != nil || manifest.Authorize(true, manifest.ThreadID, manifest.Workspace) != nil {
		t.Fatal("not a verified isolated fixture")
	}
	cfg, err := probe.Defaults()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	rpc, err := appserver.Start(ctx, probe.BundledBinary(cfg.AppPath), os.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer rpc.Close()
	thread, err := (appserver.HistoryReader{RPC: rpc.Session}).Read(ctx, manifest.ThreadID)
	if err != nil || thread.CWD != manifest.Workspace {
		t.Fatal("stored fixture identity mismatch")
	}
	matches := 0
	for _, turn := range thread.Turns {
		user, answer, usedTool := false, false, false
		for _, raw := range turn.Items {
			var item struct {
				Type, Text string
				Content    []struct{ Type, Text string }
			}
			if json.Unmarshal(raw, &item) != nil {
				t.Fatal("stored item malformed")
			}
			switch item.Type {
			case "userMessage":
				for _, part := range item.Content {
					if part.Type == "text" && strings.Contains(part.Text, marker) {
						user = true
					}
				}
			case "agentMessage":
				if strings.TrimSpace(item.Text) == marker {
					answer = true
				}
			case "commandExecution", "fileChange":
				usedTool = true
			}
		}
		if user {
			if turn.Status != "completed" || !answer || usedTool {
				t.Fatalf("marker turn was not a completed no-tool exchange: status=%s answer=%t tool=%t", turn.Status, answer, usedTool)
			}
			matches++
		}
	}
	if matches != 1 {
		t.Fatalf("expected one exact persisted exchange, found %d", matches)
	}
	t.Log("one matching completed, no-tool exchange persisted in the original fixture thread")
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
