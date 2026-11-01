// Command socialflow-lite runs the minimal SocialFlow.ai publishing service.
//
//	socialflow-lite serve                 start the API (default)
//	socialflow-lite keys create <name>    create an organisation and print its API key once
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/kael-tombo/socialflow-lite/internal/providers"
	"github.com/kael-tombo/socialflow-lite/internal/server"
	"github.com/kael-tombo/socialflow-lite/internal/store"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	args := os.Args[1:]
	var err error
	switch {
	case len(args) == 0 || args[0] == "serve":
		err = serve(log)
	case len(args) == 3 && args[0] == "keys" && args[1] == "create":
		err = createKey(args[2])
	default:
		fmt.Fprintln(os.Stderr, "usage: socialflow-lite [serve] | socialflow-lite keys create <organisation name>")
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func env(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func openStore() (*store.Store, error) {
	path := env("DATABASE_PATH", "data/socialflow-lite.db")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	return store.Open(path)
}

func createKey(name string) error {
	st, err := openStore()
	if err != nil {
		return err
	}
	defer st.Close()
	org, key, err := st.CreateOrgWithKey(context.Background(), name)
	if err != nil {
		return err
	}
	fmt.Printf("organization_id: %s\napi_key:         %s\n\nStore the key now; only its hash is kept.\n", org.ID, key)
	return nil
}

func publicURL() (string, error) {
	raw := strings.TrimRight(env("PUBLIC_URL", ""), "/")
	if raw == "" {
		return "", errors.New("PUBLIC_URL is required (the address users' browsers reach, e.g. https://social.example.com)")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "", errors.New("PUBLIC_URL is not a valid URL")
	}
	local := u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1"
	if u.Scheme != "https" && !local {
		return "", errors.New("PUBLIC_URL must use https outside localhost (OAuth callbacks require it)")
	}
	return raw, nil
}

func serve(log *slog.Logger) error {
	pub, err := publicURL()
	if err != nil {
		return err
	}
	sealer, err := store.NewSealer(env("ENCRYPTION_KEY", ""))
	if err != nil {
		return err
	}
	st, err := openStore()
	if err != nil {
		return err
	}
	defer st.Close()

	httpClient := &http.Client{Timeout: 30 * time.Second}
	s := &server.Server{
		Store:     st,
		Sealer:    sealer,
		PublicURL: pub,
		Log:       log,
		LinkedIn: &providers.LinkedIn{
			ClientID: env("LINKEDIN_CLIENT_ID", ""), ClientSecret: env("LINKEDIN_CLIENT_SECRET", ""),
			RedirectURL: pub + "/oauth/linkedin/callback", HTTP: httpClient,
			AuthBase: env("LINKEDIN_AUTH_BASE", ""), APIBase: env("LINKEDIN_API_BASE", ""),
		},
		X: &providers.X{
			ConsumerKey: env("X_CONSUMER_KEY", ""), ConsumerSecret: env("X_CONSUMER_SECRET", ""),
			CallbackURL: pub + "/oauth/x/callback", HTTP: httpClient,
			APIBase: env("X_API_BASE", ""),
		},
	}
	if !s.LinkedIn.Configured() && !s.X.Configured() {
		log.Warn("no provider configured: set LINKEDIN_CLIENT_ID/SECRET and/or X_CONSUMER_KEY/SECRET")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go s.RunScheduler(ctx, 20*time.Second)

	srv := &http.Server{
		Addr:              ":" + env("PORT", "8080"),
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      120 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()
	log.Info("socialflow-lite listening", "addr", srv.Addr, "public_url", pub,
		"linkedin", s.LinkedIn.Configured(), "x", s.X.Configured())
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
