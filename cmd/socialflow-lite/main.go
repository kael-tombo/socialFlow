// Command socialflow-lite runs the minimal SocialFlow.ai publishing service.
//
//	socialflow-lite serve                    start the API (default)
//	socialflow-lite doctor [--offline]       what to enter in the LinkedIn and X consoles, and whether it works
//	socialflow-lite keys create <name>       create an organisation and print its API key once
//	socialflow-lite keys add <org_id>        issue another key for an organisation (rotation)
//	socialflow-lite keys list                list keys (ids only; secrets are never stored)
//	socialflow-lite keys revoke <key_id>     revoke one key by the id shown in keys list
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

	"github.com/kael-tombo/socialflow-lite/internal/doctor"
	"github.com/kael-tombo/socialflow-lite/internal/providers"
	"github.com/kael-tombo/socialflow-lite/internal/server"
	"github.com/kael-tombo/socialflow-lite/internal/store"
)

const usage = `usage:
  socialflow-lite [serve]
  socialflow-lite doctor [--offline]
  socialflow-lite keys create <organisation name>
  socialflow-lite keys add <org_id>
  socialflow-lite keys list
  socialflow-lite keys revoke <key_id>`

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	args := os.Args[1:]
	var err error
	switch {
	case len(args) == 0 || args[0] == "serve":
		err = serve(log)
	case args[0] == "doctor":
		os.Exit(runDoctor(len(args) > 1 && args[1] == "--offline"))
	case len(args) == 3 && args[0] == "keys" && args[1] == "create":
		err = createKey(args[2])
	case len(args) == 3 && args[0] == "keys" && args[1] == "add":
		err = addKey(args[2])
	case len(args) == 2 && args[0] == "keys" && args[1] == "list":
		err = listKeys()
	case len(args) == 3 && args[0] == "keys" && args[1] == "revoke":
		err = revokeKey(args[2])
	default:
		fmt.Fprintln(os.Stderr, usage)
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

func withStore(f func(context.Context, *store.Store) error) error {
	st, err := openStore()
	if err != nil {
		return err
	}
	defer st.Close()
	return f(context.Background(), st)
}

func createKey(name string) error {
	return withStore(func(ctx context.Context, st *store.Store) error {
		org, key, err := st.CreateOrgWithKey(ctx, name)
		if err != nil {
			return err
		}
		fmt.Printf("organization_id: %s\napi_key:         %s\n\nStore the key now; only its hash is kept.\n", org.ID, key)
		return nil
	})
}

func addKey(orgID string) error {
	return withStore(func(ctx context.Context, st *store.Store) error {
		key, err := st.AddKey(ctx, orgID)
		if errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("no organisation %s (see keys list)", orgID)
		}
		if err != nil {
			return err
		}
		fmt.Printf("api_key: %s\n\nStore it now. Switch the client to it, then revoke the old key with keys revoke.\n", key)
		return nil
	})
}

func listKeys() error {
	return withStore(func(ctx context.Context, st *store.Store) error {
		keys, err := st.ListKeys(ctx)
		if err != nil {
			return err
		}
		if len(keys) == 0 {
			fmt.Println("No keys. Create one with: socialflow-lite keys create <organisation name>")
			return nil
		}
		fmt.Printf("%-14s %-28s %-30s %-9s %s\n", "KEY ID", "ORGANISATION", "ORG ID", "STATUS", "CREATED")
		for _, k := range keys {
			status := "active"
			if k.Revoked {
				status = "revoked"
			}
			fmt.Printf("%-14s %-28s %-30s %-9s %s\n", k.HashPrefix, truncate(k.OrgName, 28), k.OrgID, status, k.CreatedAt.Format("2006-01-02 15:04"))
		}
		return nil
	})
}

func revokeKey(id string) error {
	return withStore(func(ctx context.Context, st *store.Store) error {
		k, err := st.RevokeKey(ctx, id)
		if errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("no key %s (see keys list)", id)
		}
		if err != nil {
			return err
		}
		fmt.Printf("Revoked key %s of %s. Clients using it now get 401.\n", k.HashPrefix, k.OrgName)
		return nil
	})
}

func truncate(s string, n int) string {
	if len([]rune(s)) <= n {
		return s
	}
	return string([]rune(s)[:n-1]) + "…"
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

func newProviders(pub string, httpClient *http.Client) (*providers.LinkedIn, *providers.X) {
	return &providers.LinkedIn{
			ClientID: env("LINKEDIN_CLIENT_ID", ""), ClientSecret: env("LINKEDIN_CLIENT_SECRET", ""),
			RedirectURL: pub + "/oauth/linkedin/callback", HTTP: httpClient,
			AuthBase: env("LINKEDIN_AUTH_BASE", ""), APIBase: env("LINKEDIN_API_BASE", ""),
		}, &providers.X{
			ConsumerKey: env("X_CONSUMER_KEY", ""), ConsumerSecret: env("X_CONSUMER_SECRET", ""),
			CallbackURL: pub + "/oauth/x/callback", HTTP: httpClient,
			APIBase: env("X_API_BASE", ""),
		}
}

func runDoctor(offline bool) int {
	pub := strings.TrimRight(env("PUBLIC_URL", ""), "/")
	httpClient := &http.Client{Timeout: 20 * time.Second}
	li, x := newProviders(pub, httpClient)
	cfg := doctor.Config{PublicURL: pub, LinkedIn: li, X: x, HTTP: httpClient, SkipLiveChecks: offline}
	if _, err := store.NewSealer(env("ENCRYPTION_KEY", "")); err != nil {
		cfg.EncryptionErr = err.Error()
	} else {
		cfg.EncryptionOK = true
	}
	if st, err := openStore(); err != nil {
		cfg.DatabaseErr = err.Error()
	} else {
		cfg.DatabaseOK = true
		st.Close()
	}
	results := doctor.Run(context.Background(), cfg)
	for _, r := range results {
		fmt.Printf("[%s] %s\n         %s\n", r.Status, r.Name, r.Detail)
	}
	if doctor.Failed(results) {
		fmt.Println("\nFix the FAIL lines above, then run doctor again.")
		return 1
	}
	fmt.Println("\nNo failures. Start with: socialflow-lite serve")
	return 0
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

	li, x := newProviders(pub, &http.Client{Timeout: 30 * time.Second})
	s := &server.Server{Store: st, Sealer: sealer, PublicURL: pub, Log: log, LinkedIn: li, X: x}
	if !li.Configured() && !x.Configured() {
		log.Warn("no provider configured: set LINKEDIN_CLIENT_ID/SECRET and/or X_CONSUMER_KEY/SECRET (run `socialflow-lite doctor`)")
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
		"linkedin", li.Configured(), "x", x.Configured(),
		"linkedin_redirect_url", li.RedirectURL, "x_callback_url", x.CallbackURL)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
