package desktopagent

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/wu8685/Ariel/internal/codex/desktopipc"
	"github.com/wu8685/Ariel/internal/probe"
)

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
			t.Fatal(err)
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
