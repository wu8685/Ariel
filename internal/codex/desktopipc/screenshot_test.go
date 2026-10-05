package desktopipc

import (
	"context"
	"encoding/json"
	"testing"
)

func TestProductionScreenshotInputPreservesTextAndImageInOneTurn(t *testing.T) {
	owner := &recordingOwner{reply: Reply{Result: json.RawMessage(`{"result":{"turn":{"id":"new-turn"}}}`)}}
	idle := json.RawMessage(`{"cwd":"/fixture","threadRuntimeStatus":{"type":"idle"},"requests":[],"turns":[]}`)
	_, err := StartProductionTurnImages(context.Background(), owner, "owner", "thread", "/fixture", idle, "client-id", "caption", []string{"data:image/png;base64,AAAA"})
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(owner.calls[0].Params)
	var data struct {
		TurnStart struct {
			Request struct {
				Input []struct {
					Type string `json:"type"`
					Text string `json:"text"`
					URL  string `json:"url"`
				} `json:"input"`
			} `json:"request"`
		} `json:"turnStart"`
	}
	if json.Unmarshal(encoded, &data) != nil || len(data.TurnStart.Request.Input) != 2 || data.TurnStart.Request.Input[0].Type != "text" || data.TurnStart.Request.Input[1].Type != "image" {
		t.Fatalf("wrong native image input: %s", encoded)
	}
}
