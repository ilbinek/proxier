// Command proxier is a TCP/UDP port forwarder managed from a web UI.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"proxier/internal/api"
	"proxier/internal/app"
	"proxier/internal/auth"
	"proxier/internal/config"
	"proxier/internal/install"
	"proxier/internal/store"
	"proxier/internal/tlsx"
	"proxier/web"
)

// Set via -ldflags "-X main.version=...".
var version = "dev"

const usage = `proxier - forward TCP/UDP ports to other servers, managed from a web UI

Usage:
  proxier [serve] [flags]        run the server (default)
  proxier install [flags]        install as a systemd service (Linux, root)
  proxier uninstall [--purge]    remove the systemd service
  proxier reset-password [flags] set a new admin password and log out all sessions
  proxier version                print the version

Run "proxier serve -h" for server flags. All flags can also be set with
PROXIER_* environment variables (e.g. PROXIER_LISTEN=:8443).
`

func main() {
	args := os.Args[1:]
	cmd := "serve"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		cmd, args = args[0], args[1:]
	}
	var err error
	switch cmd {
	case "serve":
		err = serve(args)
	case "install":
		err = install.Install(args)
	case "uninstall":
		err = install.Uninstall(args)
	case "reset-password":
		err = resetPassword(args)
	case "version", "--version":
		fmt.Println("proxier", version)
	case "help", "-h", "--help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", cmd, usage)
		os.Exit(2)
	}
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(0)
		}
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func newLogger(level string) *slog.Logger {
	var l slog.Level
	if err := l.UnmarshalText([]byte(level)); err != nil {
		l = slog.LevelInfo
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: l}))
}

func serve(args []string) error {
	cfg, err := config.Parse(args)
	if err != nil {
		return err
	}
	log := newLogger(cfg.LogLevel)

	st, err := store.Open(cfg.DataDir)
	if err != nil {
		return err
	}
	defer st.Close()

	a := app.New(cfg, st, log, version)
	authSvc := auth.NewService(st)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	runCtx, cancelRun := context.WithCancel(context.Background())
	defer cancelRun()
	runDone := make(chan struct{})
	go func() { a.Run(runCtx); close(runDone) }()

	if err := a.Reload(ctx); err != nil {
		return fmt.Errorf("load rules: %w", err)
	}
	a.Emit("info", "server_started", "proxier "+version+" started", nil, "")

	tokenFile := filepath.Join(cfg.DataDir, "setup-token.txt")
	if tok, err := authSvc.SetupToken(ctx); err != nil {
		return err
	} else if tok != "" {
		_ = os.WriteFile(tokenFile, []byte(tok+"\n"), 0o600)
		log.Warn("first run: open the web UI and complete setup", "setup_token", tok, "also_in", tokenFile)
		fmt.Fprintf(os.Stderr, "\n  ┌─────────────────────────────────────────────┐\n  │  SETUP TOKEN: %-30s│\n  └─────────────────────────────────────────────┘\n\n", tok)
		go removeTokenFileAfterSetup(ctx, authSvc, tokenFile)
	} else {
		_ = os.Remove(tokenFile)
	}

	tlsCfg, err := tlsx.Config(cfg, log, app.InterfaceIPs())
	if err != nil {
		return fmt.Errorf("tls: %w", err)
	}
	handler := api.New(a, authSvc, web.Handler(), tlsCfg != nil)
	srv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           handler,
		TLSConfig:         tlsCfg,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelDebug),
	}
	ln, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		return fmt.Errorf("web UI listen %s: %w", cfg.Listen, err)
	}
	scheme := "http"
	if tlsCfg != nil {
		scheme = "https"
	}
	log.Info("web UI listening", "addr", cfg.Listen, "url", scheme+"://"+displayHost(cfg.Listen), "tls", cfg.TLSMode, "data_dir", cfg.DataDir)

	srvErr := make(chan error, 1)
	go func() {
		if tlsCfg != nil {
			srvErr <- srv.ServeTLS(ln, "", "")
		} else {
			srvErr <- srv.Serve(ln)
		}
	}()

	select {
	case <-ctx.Done():
		log.Info("shutting down")
	case err := <-srvErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("http server", "err", err)
		}
	}
	shCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(shCtx)
	a.Proxy.Shutdown()
	a.Emit("info", "server_stopped", "proxier stopped", nil, "")
	cancelRun()
	<-runDone
	return nil
}

func displayHost(listen string) string {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return listen
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		if g := app.GuessPublicAddress(); g != "" {
			host = g
		} else {
			host = "localhost"
		}
	}
	return net.JoinHostPort(host, port)
}

func removeTokenFileAfterSetup(ctx context.Context, a *auth.Service, path string) {
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if need, err := a.NeedsSetup(ctx); err == nil && !need {
				_ = os.Remove(path)
				return
			}
		}
	}
}

func resetPassword(args []string) error {
	fs := flag.NewFlagSet("reset-password", flag.ContinueOnError)
	dataDir := fs.String("data-dir", envOr("PROXIER_DATA_DIR", "./data"), "data directory")
	pw := fs.String("password", "", "new password (random if empty)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	st, err := store.Open(*dataDir)
	if err != nil {
		return err
	}
	defer st.Close()
	p := *pw
	generated := p == ""
	if generated {
		p = auth.RandomToken(15)
	}
	if err := auth.ResetPassword(context.Background(), st, p); err != nil {
		return err
	}
	if generated {
		fmt.Println("New admin password:", p)
	} else {
		fmt.Println("Admin password updated.")
	}
	fmt.Println("All sessions have been logged out.")
	return nil
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
