package probe

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/wu8685/Ariel/internal/codex/appserver"
)

// SeedFixture may only run in the App Server that created the isolated fixture.
// It does not resume existing threads, and never retries an uncertain turn/start.
func SeedFixture(ctx context.Context, rpc appserver.RPC, m Manifest, enabled bool, interval time.Duration) error {
	if err := m.Authorize(enabled, m.ThreadID, m.Workspace); err != nil {
		return err
	}
	h := appserver.HistoryReader{RPC: rpc}
	thread, err := h.Read(ctx, m.ThreadID)
	if err != nil {
		return err
	}
	if err := m.Authorize(enabled, thread.ID, thread.CWD); err != nil {
		return err
	}
	if len(thread.Turns) > 0 {
		return errors.New("seed requires an empty newly created fixture")
	}
	var accepted struct {
		Turn appserver.Turn `json:"turn"`
	}
	if err := rpc.Call(ctx, "turn/start", map[string]any{"threadId": m.ThreadID, "input": []any{map[string]any{"type": "text", "text": "This is an authorized isolated connectivity test. Do not use any tools, read or write files, or create other tasks. Reply with exactly ARIEL_SEED_OK.", "text_elements": []any{}}}}, &accepted); err != nil {
		return err
	}
	if accepted.Turn.ID == "" {
		return errors.New("seed acceptance lacks exact turn ID")
	}
	if interval <= 0 {
		interval = 500 * time.Millisecond
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			stopCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			rpc.Call(stopCtx, "turn/interrupt", map[string]string{"threadId": m.ThreadID, "turnId": accepted.Turn.ID}, nil)
			return errors.New("seed did not finish; exact-turn interrupt attempted")
		case <-ticker.C:
			thread, err := h.Read(ctx, m.ThreadID)
			if err != nil {
				return err
			}
			for _, turn := range thread.Turns {
				if turn.ID != accepted.Turn.ID {
					continue
				}
				switch turn.Status {
				case "failed", "interrupted":
					return errors.New("fixture seed did not complete successfully")
				case "completed":
					for _, body := range turn.Items {
						var item struct {
							Type string `json:"type"`
							Text string `json:"text"`
						}
						json.Unmarshal(body, &item)
						if item.Type == "agentMessage" && item.Text == "ARIEL_SEED_OK" {
							return nil
						}
					}
					return errors.New("seed completed without expected literal reply")
				}
			}
		}
	}
}
