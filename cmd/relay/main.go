package main

import (
	"context"
	"encoding/hex"
	"errors"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/wu8685/Ariel/internal/relay"
	"github.com/wu8685/Ariel/internal/webauth"
)

type config struct {
	token, webPIN, webAuth, publicOrigin, rpID, credentialsFile, setupToken, dist, listen string
	sessionKey                                                                            []byte
	origins                                                                               []string
}

type configInput struct {
	token, webPIN, webAuth, origins, publicOrigin, rpID, credentialsFile, sessionKey, setupToken, dist, listen string
}

func configFrom(token, webPIN, origins, dist, listen string) (config, error) {
	return configFromInput(configInput{token: token, webPIN: webPIN, webAuth: relay.WebAuthPIN, origins: origins, dist: dist, listen: listen})
}

func configFromInput(input configInput) (config, error) {
	if input.token == "" {
		return config{}, errors.New("ARIEL_TOKEN is required")
	}
	if input.origins == "" {
		return config{}, errors.New("ARIEL_ORIGINS is required")
	}
	if input.listen == "" {
		input.listen = "127.0.0.1:8080"
	}
	if input.dist == "" {
		input.dist = "web/dist"
	}
	if stat, err := os.Stat(filepath.Join(input.dist, "index.html")); err != nil || stat.IsDir() {
		return config{}, errors.New("built Web index.html is required; run npm run build in web/")
	}
	list := strings.Split(input.origins, ",")
	for i := range list {
		list[i] = strings.TrimSpace(list[i])
		if list[i] == "" || !strings.Contains(list[i], "://") {
			return config{}, errors.New("invalid ARIEL_ORIGINS")
		}
	}
	if input.webAuth == "" {
		input.webAuth = relay.WebAuthPIN
	}
	cfg := config{token: input.token, webPIN: input.webPIN, webAuth: input.webAuth, publicOrigin: input.publicOrigin, rpID: input.rpID, credentialsFile: input.credentialsFile, setupToken: input.setupToken, dist: input.dist, listen: input.listen, origins: list}
	switch input.webAuth {
	case relay.WebAuthPIN:
		if !relay.ValidWebPIN(input.webPIN) {
			return config{}, errors.New("ARIEL_WEB_PIN must be exactly 6 ASCII digits")
		}
		if input.token == input.webPIN {
			return config{}, errors.New("ARIEL_TOKEN and ARIEL_WEB_PIN must differ")
		}
	case relay.WebAuthPasskey:
		if input.webPIN != "" {
			return config{}, errors.New("ARIEL_WEB_PIN must not be set when ARIEL_WEB_AUTH=passkey")
		}
		origin, err := url.Parse(input.publicOrigin)
		if err != nil || origin.Scheme != "https" || origin.Host == "" || (origin.Path != "" && origin.Path != "/") || origin.RawQuery != "" || origin.Fragment != "" {
			return config{}, errors.New("ARIEL_PUBLIC_ORIGIN must be an HTTPS origin")
		}
		host := origin.Hostname()
		if input.rpID == "" || net.ParseIP(input.rpID) != nil || (host != input.rpID && !strings.HasSuffix(host, "."+input.rpID)) {
			return config{}, errors.New("ARIEL_WEBAUTHN_RP_ID must match the public host or its parent domain")
		}
		allowed := false
		for _, candidate := range list {
			allowed = allowed || candidate == input.publicOrigin
		}
		if !allowed {
			return config{}, errors.New("ARIEL_PUBLIC_ORIGIN must appear in ARIEL_ORIGINS")
		}
		if input.credentialsFile == "" {
			return config{}, errors.New("ARIEL_WEBAUTHN_CREDENTIALS_FILE is required")
		}
		key, err := hex.DecodeString(input.sessionKey)
		if err != nil || len(key) != 32 {
			return config{}, errors.New("ARIEL_SESSION_KEY must be exactly 64 hexadecimal characters")
		}
		cfg.sessionKey = key
		if _, err := os.Stat(input.credentialsFile); errors.Is(err, os.ErrNotExist) && len(input.setupToken) < 32 {
			return config{}, errors.New("ARIEL_PASSKEY_SETUP_TOKEN must contain at least 32 characters until the first Passkey is enrolled")
		}
	case "":
		panic("unreachable")
	default:
		return config{}, errors.New("ARIEL_WEB_AUTH must be pin or passkey")
	}
	return cfg, nil
}

func buildHandler(cfg config) (http.Handler, error) {
	indexHTML, err := os.ReadFile(filepath.Join(cfg.dist, "index.html"))
	if err != nil {
		return nil, errors.New("built Web index.html is required; run npm run build in web/")
	}
	var auth *webauth.Manager
	var verifier func(*http.Request) bool
	if cfg.webAuth == relay.WebAuthPasskey {
		var err error
		auth, err = webauth.New(webauth.Config{PublicOrigin: cfg.publicOrigin, RPID: cfg.rpID, CredentialsFile: cfg.credentialsFile, SessionKey: cfg.sessionKey, SetupToken: cfg.setupToken})
		if err != nil {
			return nil, err
		}
		verifier = auth.Authenticated
	}
	r, err := relay.New(relay.Config{Token: cfg.token, WebPIN: cfg.webPIN, WebAuthMode: cfg.webAuth, WebSessionVerifier: verifier, AllowedOrigins: cfg.origins})
	if err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	mux.Handle("/ws", r.Handler())
	if auth != nil {
		mux.Handle("/api/auth/", auth.Handler())
	} else {
		mux.HandleFunc("GET /api/auth/status", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Cache-Control", "no-store")
			_, _ = w.Write([]byte(`{"mode":"pin","authenticated":false,"enrollmentRequired":false}`))
		})
	}
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	files := http.FileServer(http.Dir(cfg.dist))
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/" || req.URL.Path == "/index.html" {
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(strings.ReplaceAll(string(indexHTML), "__ARIEL_WEB_AUTH__", cfg.webAuth)))
			return
		}
		files.ServeHTTP(w, req)
	}))
	return mux, nil
}

func main() {
	cfg, err := configFromInput(configInput{
		token: os.Getenv("ARIEL_TOKEN"), webPIN: os.Getenv("ARIEL_WEB_PIN"), webAuth: os.Getenv("ARIEL_WEB_AUTH"), origins: os.Getenv("ARIEL_ORIGINS"),
		publicOrigin: os.Getenv("ARIEL_PUBLIC_ORIGIN"), rpID: os.Getenv("ARIEL_WEBAUTHN_RP_ID"), credentialsFile: os.Getenv("ARIEL_WEBAUTHN_CREDENTIALS_FILE"),
		sessionKey: os.Getenv("ARIEL_SESSION_KEY"), setupToken: os.Getenv("ARIEL_PASSKEY_SETUP_TOKEN"), dist: os.Getenv("ARIEL_WEB_DIST"), listen: os.Getenv("ARIEL_LISTEN"),
	})
	if err != nil {
		log.Fatal(err)
	}
	handler, err := buildHandler(cfg)
	if err != nil {
		log.Fatal(err)
	}
	server := &http.Server{Addr: cfg.listen, Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	log.Printf("Ariel Relay listening on %s", cfg.listen)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
