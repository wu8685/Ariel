package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/wu8685/Ariel/internal/desktopagent"
	"github.com/wu8685/Ariel/internal/puck"
	"github.com/wu8685/Ariel/internal/relay"
)

const webOrigin = "http://127.0.0.1"

type stack struct {
	iso      Isolation
	token    string
	pin      string
	deviceID string
	listen   string

	module      *puck.Module
	relay       *relay.Server
	server      *http.Server
	listener    net.Listener
	agentCancel context.CancelFunc
	agentDone   chan error
}

func newStack(iso Isolation) (*stack, error) {
	if err := prepareTree(iso); err != nil {
		return nil, err
	}
	if err := bootstrapOwner(iso.HarnessDB); err != nil {
		return nil, err
	}
	ok, err := ownerBootstrapped(iso.HarnessDB)
	if err != nil || !ok {
		return nil, errors.New("owner session was not bootstrapped in the temp database")
	}
	token, err := randomHex(16)
	if err != nil {
		return nil, err
	}
	pin, err := randomPIN()
	if err != nil {
		return nil, err
	}
	return &stack{iso: iso, token: token, pin: pin, deviceID: "e2e-device"}, nil
}

func (s *stack) startRelay(ctx context.Context) error {
	r, err := relay.New(relay.Config{
		Token: s.token, WebPIN: s.pin, WebAuthMode: relay.WebAuthPIN,
		AllowedOrigins: []string{webOrigin}, PuckState: puck.StateReady, MutationTimeout: 3 * time.Minute,
	})
	if err != nil {
		return err
	}
	module, err := puck.NewModule(s.iso.PuckDir, r.PuckBridge())
	if err != nil {
		return err
	}
	r.SetPuckHandler(module)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		module.Close()
		return err
	}
	mux := http.NewServeMux()
	mux.Handle("/ws", r.Handler())
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "ok")
	})
	mux.HandleFunc("GET /api/auth/status", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"mode": "pin", "authenticated": false, "enrollmentRequired": false,
			"puck": map[string]string{"state": "ready"},
		})
	})
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = server.Serve(listener) }()
	s.module, s.relay, s.server, s.listener = module, r, server, listener
	s.listen = listener.Addr().String()
	return s.waitHealth(ctx)
}

func (s *stack) startAgent(ctx context.Context) error {
	if s.iso.CodexBinary == "" || s.iso.IPCSocket == "" {
		return errors.New("codex binary and ipc socket are required")
	}
	agentCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() {
		done <- desktopagent.Run(agentCtx, desktopagent.Config{
			URL: "ws://" + s.listen + "/ws", Token: s.token, DeviceID: s.deviceID, DeviceName: "Puck E2E",
			Binary: s.iso.CodexBinary, Socket: s.iso.IPCSocket, CWD: s.iso.AgentStateDir,
			PuckBrain: true, PuckRoot: s.iso.BrainRoot,
		})
	}()
	s.agentCancel, s.agentDone = cancel, done
	return nil
}

func (s *stack) restart(ctx context.Context) error {
	s.stopAgent()
	s.stopRelay()
	return s.startRelay(ctx)
}

func (s *stack) stopAgent() {
	if s.agentCancel != nil {
		s.agentCancel()
		select {
		case <-s.agentDone:
		case <-time.After(15 * time.Second):
		}
		s.agentCancel = nil
		s.agentDone = nil
	}
}

func (s *stack) stopRelay() {
	if s.server != nil {
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = s.server.Shutdown(shutdown)
		cancel()
		s.server = nil
	}
	if s.listener != nil {
		_ = s.listener.Close()
		s.listener = nil
	}
	if s.module != nil {
		_ = s.module.Close()
		s.module = nil
	}
	s.relay = nil
}

func (s *stack) close() {
	s.stopAgent()
	s.stopRelay()
}

func (s *stack) wsURL() string { return "ws://" + s.listen + "/ws" }

func (s *stack) waitHealth(ctx context.Context) error {
	deadline, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	for {
		req, err := http.NewRequestWithContext(deadline, http.MethodGet, "http://"+s.listen+"/healthz", nil)
		if err == nil {
			response, err := http.DefaultClient.Do(req)
			if err == nil {
				response.Body.Close()
				if response.StatusCode == http.StatusOK {
					return nil
				}
			}
		}
		if deadline.Err() != nil {
			return errors.New("isolated relay did not become healthy")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func randomHex(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", buf), nil
}

func randomPIN() (string, error) {
	var buf [4]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	value := (int(buf[0])<<16 | int(buf[1])<<8 | int(buf[2])) % 1000000
	return fmt.Sprintf("%06d", value), nil
}
