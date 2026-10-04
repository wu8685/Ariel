package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/wu8685/Ariel/internal/relay"
)

type config struct {
	token, webPIN, dist, listen string
	origins                     []string
}

func configFrom(token, webPIN, origins, dist, listen string) (config, error) {
	if token == "" {
		return config{}, errors.New("ARIEL_TOKEN is required")
	}
	if !relay.ValidWebPIN(webPIN) {
		return config{}, errors.New("ARIEL_WEB_PIN must be exactly 6 ASCII digits")
	}
	if token == webPIN {
		return config{}, errors.New("ARIEL_TOKEN and ARIEL_WEB_PIN must differ")
	}
	if origins == "" {
		return config{}, errors.New("ARIEL_ORIGINS is required")
	}
	if listen == "" {
		listen = "127.0.0.1:8080"
	}
	if dist == "" {
		dist = "web/dist"
	}
	if stat, err := os.Stat(filepath.Join(dist, "index.html")); err != nil || stat.IsDir() {
		return config{}, errors.New("built Web index.html is required; run npm run build in web/")
	}
	list := strings.Split(origins, ",")
	for i := range list {
		list[i] = strings.TrimSpace(list[i])
		if list[i] == "" || !strings.Contains(list[i], "://") {
			return config{}, errors.New("invalid ARIEL_ORIGINS")
		}
	}
	return config{token: token, webPIN: webPIN, dist: dist, listen: listen, origins: list}, nil
}

func buildHandler(cfg config) (http.Handler, error) {
	r, err := relay.New(relay.Config{Token: cfg.token, WebPIN: cfg.webPIN, AllowedOrigins: cfg.origins})
	if err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	mux.Handle("/ws", r.Handler())
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	files := http.FileServer(http.Dir(cfg.dist))
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/" || req.URL.Path == "/index.html" {
			w.Header().Set("Cache-Control", "no-store")
		}
		files.ServeHTTP(w, req)
	}))
	return mux, nil
}

func main() {
	cfg, err := configFrom(os.Getenv("ARIEL_TOKEN"), os.Getenv("ARIEL_WEB_PIN"), os.Getenv("ARIEL_ORIGINS"), os.Getenv("ARIEL_WEB_DIST"), os.Getenv("ARIEL_LISTEN"))
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
