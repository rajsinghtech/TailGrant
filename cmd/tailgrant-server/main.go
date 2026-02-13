package main

import (
	"context"
	"flag"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rajsinghtech/tailgrant/internal/config"
	"github.com/rajsinghtech/tailgrant/internal/grant"
	"github.com/rajsinghtech/tailgrant/internal/server"
	"github.com/rajsinghtech/tailgrant/internal/tsapi"
	"github.com/rajsinghtech/tailgrant/ui"
	"google.golang.org/grpc"
	"tailscale.com/tsnet"

	"go.temporal.io/sdk/client"
)

func main() {
	configPath := flag.String("config", os.Getenv("CONFIG_PATH"), "path to config file")
	flag.Parse()

	if *configPath == "" {
		slog.Error("config path required: set -config flag or CONFIG_PATH env")
		os.Exit(1)
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	grantStore, err := grant.NewYAMLGrantTypeStore(cfg.Grants)
	if err != nil {
		slog.Error("failed to create grant store", "error", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	hostname := cfg.Tailscale.Hostname
	if hostname == "" {
		hostname = "tailgrant"
	}

	srv := &tsnet.Server{
		Hostname: hostname,
	}
	if cfg.Tailscale.StateDir != "" {
		srv.Dir = cfg.Tailscale.StateDir
	}
	defer srv.Close()

	if _, err := srv.Up(ctx); err != nil {
		slog.Error("tsnet failed to start", "error", err)
		os.Exit(1)
	}
	slog.Info("tsnet is up", "hostname", hostname)

	lc, err := srv.LocalClient()
	if err != nil {
		slog.Error("failed to get local client", "error", err)
		os.Exit(1)
	}

	useTLS := cfg.Server.UseTLS == nil || *cfg.Server.UseTLS
	var ln net.Listener
	if useTLS {
		ln, err = srv.ListenTLS("tcp", cfg.Server.ListenAddr)
	} else {
		ln, err = srv.Listen("tcp", cfg.Server.ListenAddr)
	}
	if err != nil {
		slog.Error("failed to listen", "addr", cfg.Server.ListenAddr, "tls", useTLS, "error", err)
		os.Exit(1)
	}
	defer ln.Close()
	slog.Info("listening", "addr", cfg.Server.ListenAddr, "tls", useTLS)

	tc, err := client.Dial(client.Options{
		HostPort:  cfg.Temporal.Address,
		Namespace: cfg.Temporal.Namespace,
		ConnectionOptions: client.ConnectionOptions{
			DialOptions: []grpc.DialOption{
				grpc.WithContextDialer(func(ctx context.Context, addr string) (net.Conn, error) {
					return srv.Dial(ctx, "tcp", addr)
				}),
			},
		},
	})
	if err != nil {
		slog.Error("failed to connect to temporal", "error", err)
		os.Exit(1)
	}
	defer tc.Close()

	staticFS, err := fs.Sub(ui.StaticFiles, "static")
	if err != nil {
		slog.Error("failed to create static fs", "error", err)
		os.Exit(1)
	}

	tsClient := tsapi.NewClient(
		cfg.Tailscale.OAuthClientID,
		cfg.Tailscale.OAuthClientSecret,
		cfg.Tailscale.Tailnet,
	)

	router := server.NewRouter(lc, tc, tsClient, grantStore, cfg.Temporal.TaskQueue, staticFS)

	httpServer := &http.Server{Handler: router}

	go func() {
		if err := httpServer.Serve(ln); err != nil && err != http.ErrServerClosed {
			slog.Error("http server error", "error", err)
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigCh
	slog.Info("shutting down", "signal", sig)

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		slog.Error("http shutdown error", "error", err)
	}

	cancel()
}
