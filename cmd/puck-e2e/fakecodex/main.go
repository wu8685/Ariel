// Command fakecodex is a deterministic Codex app-server for puck-e2e.
// It speaks stdio JSON-RPC and never reads auth files or config.toml.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
)

func main() {
	if len(os.Args) >= 2 && os.Args[1] == "features" {
		fmt.Println("code_mode_host stable")
		return
	}
	if len(os.Args) < 2 || os.Args[1] != "app-server" {
		fmt.Fprintln(os.Stderr, "fakecodex: expected features list or app-server")
		os.Exit(2)
	}
	serve()
}

type rpc struct {
	in      *bufio.Reader
	out     *os.File
	writeMu sync.Mutex
	seq     int
}

type message struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
}

func serve() {
	peer := &rpc{in: bufio.NewReader(os.Stdin), out: os.Stdout}
	for {
		msg, err := peer.read()
		if err != nil {
			return
		}
		if msg.Method == "" {
			continue
		}
		peer.handle(msg)
	}
}

func (r *rpc) read() (message, error) {
	line, err := r.in.ReadBytes('\n')
	if err != nil {
		return message{}, err
	}
	var msg message
	if json.Unmarshal(line, &msg) != nil {
		return message{}, fmt.Errorf("bad frame")
	}
	return msg, nil
}

func (r *rpc) write(payload any) {
	body, err := json.Marshal(payload)
	if err != nil {
		return
	}
	r.writeMu.Lock()
	defer r.writeMu.Unlock()
	_, _ = r.out.Write(append(body, '\n'))
}

func (r *rpc) reply(id json.RawMessage, result any) {
	r.write(map[string]any{"id": id, "result": result})
}

func (r *rpc) fail(id json.RawMessage, code int, text string) {
	r.write(map[string]any{"id": id, "error": map[string]any{"code": code, "message": text}})
}

func (r *rpc) handle(msg message) {
	switch msg.Method {
	case "initialize":
		r.reply(msg.ID, map[string]any{"userAgent": "fakecodex"})
	case "initialized":
	case "thread/start":
		r.reply(msg.ID, map[string]any{"thread": r.startThread(msg.Params)})
	case "thread/resume":
		r.reply(msg.ID, map[string]any{"thread": threadReadView(loadThread(paramString(msg.Params, "threadId")))})
	case "thread/read":
		thread := loadThread(paramString(msg.Params, "threadId"))
		if thread.ID == "" {
			r.fail(msg.ID, -32602, "unknown thread")
			return
		}
		r.reply(msg.ID, map[string]any{"thread": threadReadView(thread)})
	case "thread/turns/list":
		thread := loadThread(paramString(msg.Params, "threadId"))
		data := make([]any, 0, len(thread.Turns))
		for index := len(thread.Turns) - 1; index >= 0; index-- {
			data = append(data, thread.Turns[index])
		}
		r.reply(msg.ID, map[string]any{"data": data})
	case "thread/list":
		archived := paramBool(msg.Params, "archived")
		data := []any{}
		for _, thread := range listThreads() {
			if thread.Archived == archived {
				data = append(data, threadReadView(thread))
			}
		}
		r.reply(msg.ID, map[string]any{"data": data})
	case "thread/search", "thread/items/list", "thread/queue/list", "model/list":
		r.reply(msg.ID, map[string]any{"data": []any{}})
	case "account/read", "config/read":
		r.reply(msg.ID, map[string]any{})
	case "thread/archive":
		r.archive(paramString(msg.Params, "threadId"), true)
		r.reply(msg.ID, map[string]any{})
	case "thread/unarchive":
		thread := r.archive(paramString(msg.Params, "threadId"), false)
		r.reply(msg.ID, map[string]any{"thread": threadView(thread)})
	case "thread/name/set":
		thread := renameThread(paramString(msg.Params, "threadId"), paramString(msg.Params, "name"))
		r.reply(msg.ID, map[string]any{"thread": threadView(thread)})
	case "turn/start":
		turnID := r.beginTurn(msg.Params)
		r.reply(msg.ID, map[string]any{"turn": map[string]any{"id": turnID}})
		if strings.Contains(paramText(msg.Params), "PUCK_ENVELOPE") {
			r.play(msg.Params, turnID)
		}
		// Let Ariel record the turn before the completion, or the processing
		// bubble stays up and the harness never sees idle.
		time.Sleep(4 * time.Second)
		r.write(map[string]any{"method": "turn/completed", "params": map[string]any{
			"threadId": paramString(msg.Params, "threadId"),
			"turnId":   turnID,
			"turn":     map[string]any{"id": turnID, "status": "completed"},
		}})
	case "turn/interrupt":
		r.reply(msg.ID, map[string]any{})
	default:
		if len(msg.ID) > 0 && string(msg.ID) != "null" {
			r.fail(msg.ID, -32601, "unsupported method")
		}
	}
}

func (r *rpc) play(params json.RawMessage, turnID string) {
	text := userRequest(paramText(params))
	threadID := paramString(params, "threadId")
	messageID := paramString(params, "clientUserMessageId")
	switch {
	case strings.Contains(text, "尚未归属"):
		title := fixtureTitle(text)
		if title == "" {
			title = "inbox"
		}
		r.saveAndAttach(threadID, title, between(text, "unassigned inbox messageId=", "\n"))
	case strings.Contains(text, "不要调用工具"):
		return
	case strings.Contains(text, "watch_thread") && strings.Contains(text, "start_task"):
		ref := uuidAfter(text, "threadId 是 ")
		cwd := lastPath(text)
		r.tool(threadID, "watch_thread", map[string]any{"threadId": ref})
		listed := r.tool(threadID, "list_devices", map[string]any{})
		deviceID := firstDevice(listed)
		started := r.tool(threadID, "start_task", map[string]any{
			"deviceId": deviceID, "cwd": cwd, "goal": "FIXTURE_HANDOFF",
			"prompt": "请阅读交接里的引用会话。", "riskTags": []any{},
			"referenceThreads": []string{ref},
		})
		rememberTask(nestedString(started, "data", "taskId"))
	case strings.Contains(text, "send_turn") && strings.Contains(text, "继续看引用"):
		taskID := rememberedTask()
		// Real runs pass taskId together with threadId, then retry once the
		// error names taskId. The retry must still carry the handoff pack.
		rejected := r.tool(threadID, "send_turn", map[string]any{"taskId": taskID, "threadId": "not-a-locator", "text": "继续看引用", "riskTags": []any{}})
		message, _ := rejected["message"].(string)
		if rejected["success"] == true || !strings.Contains(message, "taskId") {
			return
		}
		r.tool(threadID, "send_turn", map[string]any{"taskId": taskID, "text": "继续看引用", "riskTags": []any{}})
	case strings.Contains(text, "FIXTURE_Q"):
		r.saveAndAttach(threadID, fixtureTitle(text), messageID)
	case strings.Contains(text, "FIXTURE_X 的延续"):
		listed := r.tool(threadID, "list_bot_tasks", map[string]any{"query": "FIXTURE_X", "limit": 10})
		taskID := taskByTitle(listed, "FIXTURE_X")
		r.tool(threadID, "save_bot_task", map[string]any{"id": taskID})
		r.tool(threadID, "attach_bot_task", map[string]any{"taskId": taskID, "messageIds": []string{messageID}})
	case strings.Contains(text, "title 正好是 FIXTURE_"):
		r.saveAndAttach(threadID, fixtureTitle(text), messageID)
	case strings.Contains(text, "标成 done") && strings.Contains(text, "FIXTURE_X"):
		listed := r.tool(threadID, "list_bot_tasks", map[string]any{"query": "FIXTURE_X", "limit": 10})
		taskID := taskByTitle(listed, "FIXTURE_X")
		r.tool(threadID, "save_bot_task", map[string]any{"id": taskID, "title": "FIXTURE_X", "status": "done"})
	case strings.Contains(text, "FIXTURE_NOTICE"):
		r.tool(threadID, "send_message", map[string]any{"text": "FIXTURE_NOTICE"})
	default:
		_ = turnID
	}
}

func (r *rpc) saveAndAttach(threadID, title, messageID string) {
	saved := r.tool(threadID, "save_bot_task", map[string]any{"title": title, "goal": title, "status": "accepted"})
	taskID := nestedString(saved, "data", "task", "id")
	if taskID != "" && messageID != "" {
		r.tool(threadID, "attach_bot_task", map[string]any{"taskId": taskID, "messageIds": []string{messageID}})
	}
}

func (r *rpc) tool(threadID, name string, args any) map[string]any {
	r.seq++
	id := fmt.Sprintf("srv-%d", r.seq)
	encoded, _ := json.Marshal(args)
	r.write(map[string]any{
		"id": id, "method": "item/tool/call",
		"params": map[string]any{"threadId": threadID, "callId": "call-" + id, "tool": name, "arguments": json.RawMessage(encoded)},
	})
	for {
		msg, err := r.read()
		if err != nil {
			return map[string]any{}
		}
		if msg.Method != "" {
			r.handle(msg)
			continue
		}
		if sameID(msg.ID, id) {
			return toolPayload(msg.Result)
		}
	}
}

func sameID(raw json.RawMessage, id string) bool {
	var decoded any
	if json.Unmarshal(raw, &decoded) != nil {
		return false
	}
	return fmt.Sprint(decoded) == id
}

func toolPayload(raw json.RawMessage) map[string]any {
	var envelope struct {
		ContentItems []struct {
			Text string `json:"text"`
		} `json:"contentItems"`
	}
	_ = json.Unmarshal(raw, &envelope)
	if len(envelope.ContentItems) == 0 {
		return map[string]any{}
	}
	var payload map[string]any
	_ = json.Unmarshal([]byte(envelope.ContentItems[0].Text), &payload)
	if payload == nil {
		return map[string]any{}
	}
	return payload
}

func (r *rpc) startThread(params json.RawMessage) map[string]any {
	cwd := paramString(params, "cwd")
	thread := storedThread{ID: newUUID(), CWD: cwd, Name: "fake"}
	saveThread(thread)
	return threadView(thread)
}

func (r *rpc) beginTurn(params json.RawMessage) string {
	threadID := paramString(params, "threadId")
	text := paramText(params)
	turnID := newUUID()
	withState(func(state *world) {
		thread := state.Threads[threadID]
		if thread.ID == "" {
			thread.ID = threadID
			thread.CWD = paramString(params, "cwd")
		}
		inner, _ := json.Marshal(map[string]any{
			"id": newUUID(), "type": "userMessage",
			"content": []any{map[string]any{"type": "input_text", "text": text}},
		})
		item, _ := json.Marshal(map[string]any{"id": newUUID(), "type": "entry", "item": json.RawMessage(inner)})
		status, _ := json.Marshal(map[string]any{"type": "completed"})
		thread.Turns = append(thread.Turns, storedTurn{ID: turnID, Status: status, Items: []json.RawMessage{item}})
		state.Threads[threadID] = thread
	})
	return turnID
}

func (r *rpc) archive(threadID string, archived bool) storedThread {
	var thread storedThread
	withState(func(state *world) {
		thread = state.Threads[threadID]
		if thread.ID == "" {
			thread.ID = threadID
		}
		thread.Archived = archived
		state.Threads[threadID] = thread
	})
	return thread
}

func firstDevice(payload map[string]any) string {
	data, _ := payload["data"].(map[string]any)
	devices, _ := data["devices"].([]any)
	for _, raw := range devices {
		device, _ := raw.(map[string]any)
		id, _ := device["deviceId"].(string)
		if id != "" && device["agentOnline"] == true {
			return id
		}
	}
	return ""
}

func taskByTitle(payload map[string]any, title string) string {
	data, _ := payload["data"].(map[string]any)
	tasks, _ := data["tasks"].([]any)
	for _, raw := range tasks {
		task, _ := raw.(map[string]any)
		if task["title"] == title {
			id, _ := task["id"].(string)
			return id
		}
	}
	return ""
}

func nestedString(payload map[string]any, path ...string) string {
	var current any = payload
	for _, key := range path {
		object, _ := current.(map[string]any)
		current = object[key]
	}
	text, _ := current.(string)
	return text
}

func userRequest(text string) string {
	const marker = "用户请求："
	if index := strings.LastIndex(text, marker); index >= 0 {
		return text[index+len(marker):]
	}
	return text
}

func fixtureTitle(text string) string {
	const marker = "title 正好是 "
	titles := []string{"FIXTURE_Q1", "FIXTURE_Q2", "FIXTURE_Q3", "FIXTURE_X", "FIXTURE_Y", "FIXTURE_Z"}
	if index := strings.Index(text, marker); index >= 0 {
		rest := text[index+len(marker):]
		for _, title := range titles {
			if strings.HasPrefix(rest, title) {
				return title
			}
		}
	}
	for _, title := range titles {
		if strings.Contains(text, title) {
			return title
		}
	}
	return ""
}

func uuidAfter(text, marker string) string {
	index := strings.Index(text, marker)
	if index < 0 {
		return ""
	}
	rest := strings.TrimSpace(text[index+len(marker):])
	match := uuidPattern.FindString(rest)
	return match
}

func between(text, start, end string) string {
	index := strings.Index(text, start)
	if index < 0 {
		return ""
	}
	rest := text[index+len(start):]
	if cut := strings.Index(rest, end); cut >= 0 {
		rest = rest[:cut]
	}
	return strings.TrimSpace(rest)
}

func lastPath(text string) string {
	path := ""
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "/") && !strings.ContainsAny(line, " \t") {
			path = line
		}
	}
	return path
}

func paramString(raw json.RawMessage, key string) string {
	var params map[string]any
	_ = json.Unmarshal(raw, &params)
	value, _ := params[key].(string)
	return value
}

func paramBool(raw json.RawMessage, key string) bool {
	var params map[string]any
	_ = json.Unmarshal(raw, &params)
	value, _ := params[key].(bool)
	return value
}

func paramText(raw json.RawMessage) string {
	var params struct {
		Input []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"input"`
	}
	_ = json.Unmarshal(raw, &params)
	parts := make([]string, 0, len(params.Input))
	for _, item := range params.Input {
		if item.Type == "text" && item.Text != "" {
			parts = append(parts, item.Text)
		}
	}
	return strings.Join(parts, "\n")
}

var uuidPattern = regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)

func newUUID() string {
	var buf [16]byte
	f, err := os.Open("/dev/urandom")
	if err == nil {
		_, _ = f.Read(buf[:])
		_ = f.Close()
	}
	buf[6] = (buf[6] & 0x0f) | 0x40
	buf[8] = (buf[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", buf[0:4], buf[4:6], buf[6:8], buf[8:10], buf[10:16])
}
