package desktopagent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/wu8685/Ariel/internal/codex/desktopipc/testfixture"
)

func TestNativeLargeFrameOpensAndPatchesAtProductionLimits(t *testing.T) {
	resetFrameHints()
	o, err := testfixture.NewLargeOwner()
	if err != nil {
		t.Fatal(err)
	}
	defer o.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	live, err := OpenFollower(ctx, o.Path, testfixture.ThreadID, testfixture.CWD)
	if err != nil {
		t.Fatalf("production-limit native cold open failed: %v", err)
	}
	defer live.Close()
	check := func(text string) bool {
		t.Helper()
		state, err := live.Current()
		if err != nil {
			t.Fatalf("native follower invalidated: %v", err)
		}
		var got struct {
			Unknown string `json:"fixtureUnknown"`
			Schema  struct {
				Type []string `json:"type"`
			} `json:"fixtureSchema"`
			Turns []struct {
				Items []struct {
					Result  json.RawMessage `json:"result"`
					Content []struct {
						URL string `json:"url"`
					} `json:"content"`
				} `json:"items"`
			} `json:"turns"`
		}
		if err := json.Unmarshal(state, &got); err != nil {
			t.Fatal(err)
		}
		if got.Unknown != testfixture.UnknownText() {
			t.Fatal("projection modified unknown content")
		}
		if len(got.Schema.Type) != 2 || got.Schema.Type[0] != "string" || got.Schema.Type[1] != "null" {
			t.Fatal("projection modified retained JSON schema type")
		}
		if len(state) >= 2<<20 {
			t.Fatalf("lazy projection or literal encoding regressed: resident=%d, want <2 MiB", len(state))
		}
		if len(got.Turns) != 1 || len(got.Turns[0].Items) != 3 || len(got.Turns[0].Items[0].Content) != 2 {
			t.Fatal("projection changed native item structure")
		}
		if string(got.Turns[0].Items[2].Result) != `"__ARIEL_LAZY_OMITTED__"` || got.Turns[0].Items[0].Content[1].URL != "__ARIEL_LAZY_OMITTED__" {
			t.Fatal("tool and image bodies must both be lazy")
		}
		if _, err := NormalizeLive(testfixture.ThreadID, "fixture", testfixture.CWD, state); err != nil {
			t.Fatalf("native state could not reach Agent normalization: %v", err)
		}
		return strings.Contains(string(state), text)
	}
	if !check(testfixture.InitialText) {
		t.Fatal("initial text missing")
	}
	stats := o.Stats()
	if stats.WireBytes <= 8<<20 || stats.WireBytes >= 64<<20 || stats.Loads != 2 || stats.Connections != 2 {
		t.Fatalf("did not exercise production small→large fallback: %+v", stats)
	}
	for _, step := range []struct {
		complete bool
		text     string
	}{{false, testfixture.StreamingText}, {true, testfixture.CompletedText}} {
		if err := o.Advance(step.complete); err != nil {
			t.Fatal(err)
		}
		for !check(step.text) {
			select {
			case <-live.Updates():
			case <-ctx.Done():
				t.Fatal("native patch did not arrive")
			}
		}
	}
	if after := o.Stats(); after.Loads != stats.Loads || after.Connections != stats.Connections || after.Patches != 2 || after.Unexpected != 0 {
		t.Fatalf("updates reloaded history, reattached or attempted mutations: %+v", after)
	}
	t.Logf("native wire=%d bytes, 8→64 MiB fallback, two ordered patches, zero mutation", stats.WireBytes)
}
