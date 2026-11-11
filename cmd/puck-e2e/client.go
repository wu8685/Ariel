package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

type client struct {
	conn   *websocket.Conn
	origin string
	seq    int
}

func dialOwner(ctx context.Context, url, origin, pin string) (*client, error) {
	header := http.Header{"Origin": []string{origin}}
	conn, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{HTTPHeader: header})
	if err != nil {
		return nil, err
	}
	owner := &client{conn: conn, origin: origin}
	if err := wsjson.Write(ctx, conn, map[string]any{"type": "hello", "v": 1, "role": "web", "token": pin}); err != nil {
		conn.CloseNow()
		return nil, err
	}
	hello, err := owner.read(ctx, "")
	if err != nil {
		conn.CloseNow()
		return nil, err
	}
	if hello["type"] != "hello.ok" {
		conn.CloseNow()
		return nil, fmt.Errorf("owner hello rejected")
	}
	return owner, nil
}

func (c *client) Close() {
	if c != nil && c.conn != nil {
		c.conn.CloseNow()
	}
}

func (c *client) call(ctx context.Context, method, deviceID string, params any, timeout time.Duration) (map[string]any, error) {
	c.seq++
	requestID := fmt.Sprintf("00000000-0000-4000-8000-%012d", c.seq)
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if err := wsjson.Write(callCtx, c.conn, map[string]any{
		"type": "request", "v": 1, "requestId": requestID, "deviceId": deviceID, "method": method, "params": params,
	}); err != nil {
		return nil, err
	}
	return c.read(callCtx, requestID)
}

func (c *client) read(ctx context.Context, requestID string) (map[string]any, error) {
	for {
		var value map[string]any
		if err := wsjson.Read(ctx, c.conn, &value); err != nil {
			return nil, err
		}
		if requestID == "" && value["type"] == "hello.ok" {
			return value, nil
		}
		if requestID != "" && value["requestId"] == requestID {
			return value, nil
		}
	}
}

func dataOf(response map[string]any) map[string]any {
	data, _ := response["data"].(map[string]any)
	if data == nil {
		return map[string]any{}
	}
	return data
}

func outcomeCode(response map[string]any) string {
	if response["outcome"] == "accepted" {
		return "accepted"
	}
	problem, _ := response["error"].(map[string]any)
	code, _ := problem["code"].(string)
	if code == "" {
		code = "rejected"
	}
	return code
}

func newID() string {
	var buf [16]byte
	_, _ = rand.Read(buf[:])
	buf[6] = (buf[6] & 0x0f) | 0x40
	buf[8] = (buf[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", buf[0:4], buf[4:6], buf[6:8], buf[8:10], buf[10:16])
}

func messagesOf(response map[string]any) []map[string]any {
	raw, _ := json.Marshal(dataOf(response)["messages"])
	var messages []map[string]any
	_ = json.Unmarshal(raw, &messages)
	return messages
}
