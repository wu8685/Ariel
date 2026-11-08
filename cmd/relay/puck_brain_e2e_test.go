package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/wu8685/Ariel/internal/desktopagent"
	"github.com/wu8685/Ariel/internal/puck"
	"github.com/wu8685/Ariel/internal/relay"
)

// This opt-in smoke owns a new Relay state directory, Puck brain workspace,
// Bot and execution thread. It never resumes or mutates an existing thread.
func TestPuckBrainFirstMessageFullModeRemainsOneTask(t *testing.T) {
	runPuckBrainFirstMessageMode(t, "full")
}

func TestPuckBrainFirstMessageAskModeRemainsOneAuthorization(t *testing.T) {
	runPuckBrainFirstMessageMode(t, "ask")
}

func runPuckBrainFirstMessageMode(t *testing.T, mode string) {
	if os.Getenv("ARIEL_TEST_REAL_PUCK_E2E") != "1" {
		t.Skip("set ARIEL_TEST_REAL_PUCK_E2E=1 for the isolated release smoke")
	}
	binary := os.Getenv("ARIEL_CODEX_BINARY")
	if binary == "" {
		t.Fatal("ARIEL_CODEX_BINARY is required")
	}
	socket := os.Getenv("ARIEL_IPC_SOCKET")
	if socket == "" {
		socket = filepath.Join(os.Getenv("HOME"), ".codex", "ipc", "ipc.sock")
	}
	root := t.TempDir()
	fixture := filepath.Join(root, "execution-fixture")
	fixtureChild := filepath.Join(fixture, "child")
	if err := os.MkdirAll(fixtureChild, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixture, "marker.txt"), []byte("fixture-marker\n"), 0600); err != nil {
		t.Fatal(err)
	}
	const token, pin, origin = "fixture-agent-token", "012345", "http://localhost:5173"
	r, err := relay.New(relay.Config{Token: token, WebPIN: pin, WebAuthMode: relay.WebAuthPIN, AllowedOrigins: []string{origin}, PuckState: puck.StateReady, MutationTimeout: 3 * time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	module, err := puck.NewModule(filepath.Join(root, "relay-state"), r.PuckBridge())
	if err != nil {
		t.Fatal(err)
	}
	defer module.Close()
	r.SetPuckHandler(module)
	server := httptest.NewServer(r.Handler())
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	agentDone := make(chan error, 1)
	go func() {
		agentDone <- desktopagent.Run(ctx, desktopagent.Config{
			URL:                      "ws" + strings.TrimPrefix(server.URL, "http") + "/ws",
			Token:                    token,
			DeviceID:                 "fixture-device",
			DeviceName:               "Fixture Device",
			Binary:                   binary,
			Socket:                   socket,
			CWD:                      fixture,
			PuckBrain:                true,
			PuckRoot:                 filepath.Join(root, "brain"),
			DisableExecutionSettings: false,
		})
	}()

	webURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"
	header := http.Header{"Origin": []string{origin}}
	web, _, err := websocket.Dial(ctx, webURL, &websocket.DialOptions{HTTPHeader: header})
	if err != nil {
		t.Fatal(err)
	}
	defer web.CloseNow()
	writeE2EJSON(t, web, map[string]any{"type": "hello", "v": 1, "role": "web", "token": pin})
	readE2EResponse(t, web, "", 15*time.Second)
	requestSequence := 0
	nextRequestID := func() string {
		requestSequence++
		return fmt.Sprintf("00000000-0000-4000-8000-%012d", requestSequence)
	}

	deviceDeadline := time.Now().Add(90 * time.Second)
	for {
		response := e2eRequest(t, web, nextRequestID(), "device.list", "relay", map[string]any{}, 15*time.Second)
		if hasReadyFixtureDevice(response) {
			break
		}
		if time.Now().After(deviceDeadline) {
			t.Fatal("isolated Desktop Agent did not become Puck-ready")
		}
		time.Sleep(time.Second)
	}

	created := e2eRequest(t, web, nextRequestID(), "puck.create", "relay", map[string]any{
		"name": "Fixture Assistant", "brainDeviceId": "fixture-device", "permissionMode": mode,
	}, 3*time.Minute)
	assistant := responseData(created)["assistant"].(map[string]any)
	assistantID := assistant["assistantId"].(string)
	greetingDeadline := time.Now().Add(4 * time.Minute)
	for {
		history := e2eRequest(t, web, nextRequestID(), "puck.history", "relay", map[string]any{"assistantId": assistantID}, 15*time.Second)
		if e2eGreetingCompleted(history) {
			break
		}
		if time.Now().After(greetingDeadline) {
			t.Fatal("isolated Bot greeting did not complete")
		}
		time.Sleep(2 * time.Second)
	}
	// The durable assistant bubble precedes app-server turn/completed by a
	// small interval. Avoid turning that presentation event into a busy-turn
	// race in this release smoke.
	time.Sleep(10 * time.Second)
	reconnectedDeadline := time.Now().Add(90 * time.Second)
	consecutiveReady := 0
	for {
		devices := e2eRequest(t, web, nextRequestID(), "device.list", "relay", map[string]any{}, 15*time.Second)
		if hasReadyFixtureDevice(devices) {
			consecutiveReady++
			if consecutiveReady == 3 {
				break
			}
		} else {
			consecutiveReady = 0
		}
		if time.Now().After(reconnectedDeadline) {
			t.Fatal("isolated Desktop Agent did not become ready after greeting")
		}
		time.Sleep(time.Second)
	}
	messageID := "00000000-0000-4000-8000-0000000009" + map[string]string{"full": "11", "ask": "12"}[mode]
	prompt := "请在 fixture-device 的目录 " + fixture + " 实际检查 marker.txt 是否存在并报告结果；必须交给目标设备执行，不得猜测，不读取其他目录、不修改文件。"
	sent := e2eRequest(t, web, nextRequestID(), "puck.message.send", "relay", map[string]any{
		"assistantId": assistantID, "clientMessageId": messageID, "text": prompt,
	}, 3*time.Minute)
	if sent["outcome"] != "accepted" {
		select {
		case agentErr := <-agentDone:
			t.Fatalf("message rejected after Agent stopped (%v): %v", agentErr, sent["error"])
		default:
			t.Fatalf("message rejected: %v", sent["error"])
		}
	}

	deadline := time.Now().Add(6 * time.Minute)
	var firstExpectedAt time.Time
	lastTasks, lastAuthorizations := 0, 0
	var lastAssistantKinds []string
	for {
		history := e2eRequest(t, web, nextRequestID(), "puck.history", "relay", map[string]any{"assistantId": assistantID}, 15*time.Second)
		tasks, authorizations := e2eMessageKinds(history)
		lastTasks, lastAuthorizations = tasks, authorizations
		lastAssistantKinds = e2eAssistantKinds(history)
		expected := mode == "full" && tasks == 1 && authorizations == 0 || mode == "ask" && tasks == 0 && authorizations == 1
		if expected && firstExpectedAt.IsZero() {
			firstExpectedAt = time.Now()
			if mode == "ask" {
				break
			}
			deadline = firstExpectedAt.Add(3*time.Minute + 30*time.Second)
		}
		if !firstExpectedAt.IsZero() && !expected {
			t.Fatalf("mode=%s changed after first expected state: tasks=%d authorizations=%d", mode, tasks, authorizations)
		}
		if !firstExpectedAt.IsZero() && time.Since(firstExpectedAt) >= 3*time.Minute {
			t.Logf("mode=%s stable for full behavior window: tasks=%d authorizations=%d", mode, tasks, authorizations)
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("mode=%s did not reach expected state: tasks=%d authorizations=%d assistantKinds=%v", mode, lastTasks, lastAuthorizations, lastAssistantKinds)
		}
		time.Sleep(2 * time.Second)
	}
	if mode == "ask" {
		history := e2eRequest(t, web, nextRequestID(), "puck.history", "relay", map[string]any{"assistantId": assistantID}, 15*time.Second)
		authorizationID := e2ePendingAuthorizationID(history)
		if authorizationID == "" {
			t.Fatal("ask mode reached authorization count without a pending authorization id")
		}
		decided := e2eRequest(t, web, nextRequestID(), "puck.authorization.decide", "relay", map[string]any{"assistantId": assistantID, "authorizationId": authorizationID, "decision": "approve_always"}, 3*time.Minute)
		if decided["outcome"] != "accepted" {
			t.Fatalf("approve_always rejected: %v", decided["error"])
		}
		e2eWaitForPuckCounts(t, web, nextRequestID, assistantID, 1, 1, false, 3*time.Minute)
		e2eWaitForBrainIdle(t, web, nextRequestID, assistantID, 3*time.Minute)
		rulesResponse := e2eRequest(t, web, nextRequestID(), "puck.authorization.rule.list", "relay", map[string]any{"assistantId": assistantID}, 15*time.Second)
		ruleID := e2eFirstRuleID(rulesResponse)
		if ruleID == "" {
			t.Fatal("approve_always did not persist a rule")
		}
		secondID := "00000000-0000-4000-8000-000000000913"
		secondPrompt := "请在 fixture-device 的目录 " + fixtureChild + " 实际检查 marker.txt 的上级目录内容并报告；必须交给目标设备执行，不修改文件。"
		if response := e2eRequest(t, web, nextRequestID(), "puck.message.send", "relay", map[string]any{"assistantId": assistantID, "clientMessageId": secondID, "text": secondPrompt}, 3*time.Minute); response["outcome"] != "accepted" {
			t.Fatalf("second ask message rejected: %v", response["error"])
		}
		e2eWaitForPuckCounts(t, web, nextRequestID, assistantID, 2, 1, true, 6*time.Minute)
		e2eWaitForBrainIdle(t, web, nextRequestID, assistantID, 3*time.Minute)
		revoked := e2eRequest(t, web, nextRequestID(), "puck.authorization.rule.revoke", "relay", map[string]any{"assistantId": assistantID, "ruleId": ruleID}, 15*time.Second)
		if revoked["outcome"] != "accepted" {
			t.Fatalf("rule revoke rejected: %v", revoked["error"])
		}
		thirdID := "00000000-0000-4000-8000-000000000914"
		thirdPrompt := "请再次在 fixture-device 的目录 " + fixtureChild + " 实际检查 marker.txt 的上级目录内容并报告；必须交给目标设备执行，不修改文件。"
		if response := e2eRequest(t, web, nextRequestID(), "puck.message.send", "relay", map[string]any{"assistantId": assistantID, "clientMessageId": thirdID, "text": thirdPrompt}, 3*time.Minute); response["outcome"] != "accepted" {
			t.Fatalf("third ask message rejected: %v", response["error"])
		}
		e2eWaitForPuckCounts(t, web, nextRequestID, assistantID, 2, 2, true, 6*time.Minute)
		t.Log("mode=ask persistent authorization flow: first=0 task/1 authorization, approved=1/1, automatic=2/1, revoked=2/2")
	}
	_ = e2eRequest(t, web, nextRequestID(), "puck.stop", "relay", map[string]any{"assistantId": assistantID}, 30*time.Second)
	cancel()
	select {
	case <-agentDone:
	case <-time.After(10 * time.Second):
		t.Fatal("isolated Desktop Agent did not stop")
	}
}

func writeE2EJSON(t *testing.T, conn *websocket.Conn, value any) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := wsjson.Write(ctx, conn, value); err != nil {
		t.Fatal(err)
	}
}

func readE2EResponse(t *testing.T, conn *websocket.Conn, requestID string, timeout time.Duration) map[string]any {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	for {
		var value map[string]any
		if err := wsjson.Read(ctx, conn, &value); err != nil {
			t.Fatal(err)
		}
		if requestID == "" || value["requestId"] == requestID {
			return value
		}
	}
}

func e2eRequest(t *testing.T, conn *websocket.Conn, requestID, method, deviceID string, params any, timeout time.Duration) map[string]any {
	t.Helper()
	writeE2EJSON(t, conn, map[string]any{"type": "request", "v": 1, "requestId": requestID, "deviceId": deviceID, "method": method, "params": params})
	return readE2EResponse(t, conn, requestID, timeout)
}

func responseData(response map[string]any) map[string]any {
	data, _ := response["data"].(map[string]any)
	return data
}

func hasReadyFixtureDevice(response map[string]any) bool {
	devices, _ := responseData(response)["devices"].([]any)
	for _, raw := range devices {
		device, _ := raw.(map[string]any)
		capabilities, _ := device["capabilities"].(map[string]any)
		if device["deviceId"] == "fixture-device" && capabilities["puckBrainReady"] == true && capabilities["threadBootstrap"] == true && capabilities["executionPolicy"] == true {
			return true
		}
	}
	return false
}

func e2eMessageKinds(response map[string]any) (tasks, authorizations int) {
	messages, _ := responseData(response)["messages"].([]any)
	for _, raw := range messages {
		message, _ := raw.(map[string]any)
		switch message["kind"] {
		case "task":
			tasks++
		case "authorization":
			authorizations++
		}
	}
	return tasks, authorizations
}

func e2ePendingAuthorizationID(response map[string]any) string {
	messages, _ := responseData(response)["messages"].([]any)
	for _, raw := range messages {
		message, _ := raw.(map[string]any)
		if message["kind"] != "authorization" {
			continue
		}
		var card map[string]any
		_ = json.Unmarshal([]byte(fmt.Sprint(message["cardJSON"])), &card)
		if card["cardState"] == "pending" {
			return fmt.Sprint(card["authorizationId"])
		}
	}
	return ""
}

func e2eFirstRuleID(response map[string]any) string {
	rules, _ := responseData(response)["rules"].([]any)
	if len(rules) == 0 {
		return ""
	}
	rule, _ := rules[0].(map[string]any)
	return fmt.Sprint(rule["id"])
}

func e2eWaitForPuckCounts(t *testing.T, web *websocket.Conn, nextRequestID func() string, assistantID string, wantTasks, wantAuthorizations int, requireAutoRecord bool, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		history := e2eRequest(t, web, nextRequestID(), "puck.history", "relay", map[string]any{"assistantId": assistantID}, 15*time.Second)
		tasks, authorizations := e2eMessageKinds(history)
		autoRecord := !requireAutoRecord || e2eHasAutomaticAuthorizationRecord(history)
		if tasks == wantTasks && authorizations == wantAuthorizations && autoRecord {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("counts did not reach tasks=%d authorizations=%d autoRecord=%v; got tasks=%d authorizations=%d autoRecord=%v", wantTasks, wantAuthorizations, requireAutoRecord, tasks, authorizations, autoRecord)
		}
		time.Sleep(2 * time.Second)
	}
}

func e2eHasAutomaticAuthorizationRecord(response map[string]any) bool {
	messages, _ := responseData(response)["messages"].([]any)
	for _, raw := range messages {
		message, _ := raw.(map[string]any)
		if strings.Contains(fmt.Sprint(message["text"]), "以后都允许") {
			return true
		}
	}
	return false
}

func e2eWaitForBrainIdle(t *testing.T, web *websocket.Conn, nextRequestID func() string, assistantID string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		history := e2eRequest(t, web, nextRequestID(), "puck.history", "relay", map[string]any{"assistantId": assistantID}, 15*time.Second)
		busy := false
		for _, kind := range e2eAssistantKinds(history) {
			busy = busy || kind == "processing" || kind == "greeting_processing"
		}
		if !busy {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("Brain did not become idle")
		}
		time.Sleep(2 * time.Second)
	}
}

func e2eAssistantKinds(response map[string]any) []string {
	messages, _ := responseData(response)["messages"].([]any)
	var kinds []string
	for _, raw := range messages {
		message, _ := raw.(map[string]any)
		if message["role"] == "assistant" {
			kinds = append(kinds, fmt.Sprint(message["kind"]))
		}
	}
	return kinds
}

func e2eGreetingCompleted(response map[string]any) bool {
	messages, _ := responseData(response)["messages"].([]any)
	for _, raw := range messages {
		message, _ := raw.(map[string]any)
		if message["role"] == "assistant" && message["kind"] == "text" {
			return true
		}
	}
	return false
}
