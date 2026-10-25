package appserver

import (
	"context"
	"testing"
)

func TestRestartingRPCAdmitsOnlyOfficialGateReadMethods(t *testing.T) {
	endpoint := &fakeReadOnlyEndpoint{done: make(chan struct{})}
	rpc, err := NewRestartingRPC(context.Background(), func(context.Context) (ReadOnlyEndpoint, error) { return endpoint, nil })
	if err != nil {
		t.Fatal(err)
	}
	defer rpc.Close()
	for _, method := range []string{"account/read", "config/read", "model/list"} {
		var result struct{ OK bool }
		if err := rpc.Call(context.Background(), method, map[string]any{}, &result); err != nil || !result.OK {
			t.Fatalf("%s: %v", method, err)
		}
	}
	for _, method := range []string{"account/logout", "account/login/start", "config/value/write"} {
		if err := rpc.Call(context.Background(), method, nil, nil); err == nil {
			t.Fatalf("mutation admitted: %s", method)
		}
	}
}
