package main

import (
	"context"
	"flag"
	"fmt"
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

	tsClient := tsapi.NewClient(
		cfg.Tailscale.OAuthClientID,
		cfg.Tailscale.OAuthClientSecret,
		cfg.Tailscale.Tailnet,
	)

	srv := &tsnet.Server{
		Hostname:     hostname,
		ClientSecret: cfg.Tailscale.OAuthClientSecret + "?ephemeral=false&preauthorized=true",
	}
	if cfg.Tailscale.StateDir != "" {
		srv.Dir = cfg.Tailscale.StateDir
	}
	if len(cfg.Server.Tags) > 0 {
		srv.AdvertiseTags = cfg.Server.Tags
	}
	defer func() { _ = srv.Close() }()

	if _, err := srv.Up(ctx); err != nil {
		slog.Error("tsnet failed to start", "error", err)
		os.Exit(1)
	}
	slog.Info("tsnet is up", "hostname", hostname)

	if cfg.Temporal.UseTsnet {
		dialCtx, dialCancel := context.WithTimeout(ctx, 30*time.Second)
		conn, err := srv.Dial(dialCtx, "tcp", cfg.Temporal.Address)
		dialCancel()
		if err != nil {
			slog.Error("tsnet cannot reach temporal", "address", cfg.Temporal.Address, "error", err)
			os.Exit(1)
		}
		conn.Close()
		slog.Info("tsnet connectivity verified", "address", cfg.Temporal.Address)
	}

	lc, err := srv.LocalClient()
	if err != nil {
		slog.Error("failed to get local client", "error", err)
		os.Exit(1)
	}

	// Regular tsnet listener
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
	defer func() { _ = ln.Close() }()
	slog.Info("listening", "addr", cfg.Server.ListenAddr, "tls", useTLS)

	// VIP service listener (optional)
	var svcLn net.Listener
	if svcCfg := cfg.Server.Service; svcCfg != nil {
		vipOps := tsapi.NewVIPServiceOperations(tsClient)
		if err := ensureVIPService(ctx, vipOps, svcCfg); err != nil {
			slog.Error("failed to ensure VIP service", "name", svcCfg.Name, "error", err)
			os.Exit(1)
		}

		var mode tsnet.ServiceMode
		if svcCfg.HTTPS {
			mode = tsnet.ServiceModeHTTP{Port: svcCfg.Port, HTTPS: true}
		} else {
			mode = tsnet.ServiceModeTCP{Port: svcCfg.Port}
		}
		sl, err := srv.ListenService(svcCfg.Name, mode)
		if err != nil {
			slog.Error("failed to listen on VIP service", "name", svcCfg.Name, "error", err)
			os.Exit(1)
		}
		svcLn = sl
		defer func() { _ = sl.Close() }()
		slog.Info("VIP service listening", "name", svcCfg.Name, "fqdn", sl.FQDN, "port", svcCfg.Port)
	}

	temporalOpts := client.Options{
		HostPort:  cfg.Temporal.Address,
		Namespace: cfg.Temporal.Namespace,
	}
	if cfg.Temporal.UseTsnet {
		slog.Info("using tsnet dialer for temporal", "address", cfg.Temporal.Address)
		temporalOpts.HostPort = "passthrough:///" + cfg.Temporal.Address
		temporalOpts.ConnectionOptions = client.ConnectionOptions{
			DialOptions: []grpc.DialOption{
				grpc.WithContextDialer(func(ctx context.Context, addr string) (net.Conn, error) {
					return srv.Dial(ctx, "tcp", addr)
				}),
			},
		}
	}
	tc, err := client.NewLazyClient(temporalOpts)
	if err != nil {
		slog.Error("failed to create temporal client", "error", err)
		os.Exit(1)
	}
	defer tc.Close()

	staticFS, err := fs.Sub(ui.StaticFiles, "static")
	if err != nil {
		slog.Error("failed to create static fs", "error", err)
		os.Exit(1)
	}

	router := server.NewRouter(lc, tc, tsClient, grantStore, cfg.Temporal.TaskQueue, staticFS)

	httpServer := &http.Server{Handler: router}

	go func() {
		if err := httpServer.Serve(ln); err != nil && err != http.ErrServerClosed {
			slog.Error("http server error", "error", err)
		}
	}()

	if svcLn != nil {
		svcServer := &http.Server{Handler: router}
		go func() {
			if err := svcServer.Serve(svcLn); err != nil && err != http.ErrServerClosed {
				slog.Error("VIP service http error", "error", err)
			}
		}()
		defer func() {
			shutCtx, c := context.WithTimeout(context.Background(), 10*time.Second)
			defer c()
			_ = svcServer.Shutdown(shutCtx)
		}()
	}

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

func ensureVIPService(ctx context.Context, vipOps *tsapi.VIPServiceOperations, svcCfg *config.ServiceConfig) error {
	existing, err := vipOps.Get(ctx, svcCfg.Name)
	if err != nil {
		return err
	}

	svc := tsapi.VIPService{
		Name:    svcCfg.Name,
		Comment: svcCfg.Comment,
		Tags:    svcCfg.Tags,
		Ports:   []string{fmt.Sprintf("tcp:%d", svcCfg.Port)},
	}
	if existing != nil {
		svc.Addrs = existing.Addrs
	}

	slog.Info("ensuring VIP service exists", "name", svcCfg.Name)
	return vipOps.CreateOrUpdate(ctx, svc)
}
