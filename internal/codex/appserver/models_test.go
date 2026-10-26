package appserver

import (
	"context"
	"encoding/json"
	"testing"
)

type modelRPC struct {
	calls int
	loop  bool
}

func (f *modelRPC) Call(_ context.Context, method string, p any, out any) error {
	f.calls++
	if method != "model/list" {
		panic("unexpected method")
	}
	body := `{"data":[{"id":"m","model":"m","displayName":"Model","description":"desc","supportedReasoningEfforts":[{"reasoningEffort":"deep","description":"Deep"}],"defaultReasoningEffort":"deep","inputModalities":["text"],"serviceTiers":[{"id":"native-priority","name":"Fast","description":"more usage"}],"isDefault":true}],"nextCursor":null}`
	if f.loop {
		body = `{"data":[],"nextCursor":"same"}`
	}
	return json.Unmarshal([]byte(body), out)
}
func TestModelCatalogUsesAdvertisedIDsAndBoundsPagination(t *testing.T) {
	rpc := &modelRPC{}
	catalog, err := (ModelReader{RPC: rpc}).Models(context.Background())
	if err != nil || len(catalog.Models) != 1 || catalog.Models[0].Tiers[0].ID != "native-priority" || catalog.Models[0].Efforts[0].ID != "deep" {
		t.Fatalf("catalog=%+v err=%v", catalog, err)
	}
	rpc = &modelRPC{loop: true}
	if _, err = (ModelReader{RPC: rpc}).Models(context.Background()); err == nil || rpc.calls > 8 {
		t.Fatalf("pagination unbounded: %d %v", rpc.calls, err)
	}
}
