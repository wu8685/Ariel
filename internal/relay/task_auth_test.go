package relay

import (
	"context"
	"github.com/coder/websocket/wsjson"
	"net/http/httptest"
	"testing"
	"time"
)

func TestTaskDeviceCredentialsBindIdentityAndReserveTaskIDs(t *testing.T) {
	managerToken := "manager-specific-high-entropy-token-00000000001"
	cfg := Config{Token: "legacy-shared", WebPIN: "012345", AllowedOrigins: []string{testOrigin}, TasksEnabled: true, TaskDevices: map[string]TaskDeviceCredential{
		"manager":  {Token: managerToken, TaskStoreIDs: []string{"store"}, ExecutionTargets: []string{"executor"}},
		"executor": {Token: "executor-specific-high-entropy-token-000000001"},
	}}
	r, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	s := httptest.NewServer(r.Handler())
	defer s.Close()
	for _, test := range []struct {
		id, token string
		allowed   bool
	}{
		{"manager", "legacy-shared", false}, {"executor", managerToken, false}, {"manager", managerToken, true}, {"ordinary", "legacy-shared", true},
	} {
		conn := dialTest(t, s.URL, "")
		hello := agentHello()
		hello["deviceId"] = test.id
		hello["token"] = test.token
		sendJSON(t, conn, hello)
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		var result map[string]any
		err := wsjson.Read(ctx, conn, &result)
		cancel()
		conn.CloseNow()
		if (err == nil) != test.allowed {
			t.Fatalf("device=%s allowed=%v err=%v", test.id, test.allowed, err)
		}
	}
}

func TestTasksDisabledDoesNotLoadDeviceCredentialFile(t *testing.T) {
	devices, err := LoadTaskDeviceCredentials(false, "/does-not-exist/tasks.json")
	if err != nil || len(devices) != 0 {
		t.Fatalf("disabled touched credential file: %v", err)
	}
}
