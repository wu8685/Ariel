package relay

import (
	"net/http/httptest"
	"runtime"
	"testing"
)

// An online/offline broadcast can race a newly authenticated Web connection.
// The strict Web client must always see hello.ok before any device event.
func TestRelayHelloPrecedesConcurrentDeviceBroadcasts(t *testing.T) {
	r, err := New(Config{Token: "test-token", WebPIN: "012345", AllowedOrigins: []string{testOrigin}})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(r.Handler())
	defer srv.Close()
	stop, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			default:
				r.broadcastDevice("mock-mac", &peer{deviceID: "mock-mac"})
				runtime.Gosched()
			}
		}
	}()
	defer func() { close(stop); <-done }()
	for i := 0; i < 64; i++ {
		web := dialTest(t, srv.URL, testOrigin)
		sendJSON(t, web, map[string]any{"type": "hello", "v": 1, "role": "web", "token": "012345"})
		first := readJSON(t, web)
		web.CloseNow()
		if first["type"] != "hello.ok" {
			t.Fatalf("broadcast preceded hello at connection %d: type=%v event=%v", i, first["type"], first["event"])
		}
	}
}
