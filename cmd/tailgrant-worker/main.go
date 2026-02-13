package main

import (
	"context"
	"flag"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/rajsinghtech/tailgrant/internal/config"
	"github.com/rajsinghtech/tailgrant/internal/grant"
	"github.com/rajsinghtech/tailgrant/internal/tsapi"
	"google.golang.org/grpc"
	"tailscale.com/tsnet"

	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	hostname := cfg.Tailscale.Hostname
	if hostname == "" {
		hostname = "tailgrant"
	}

	stateDir := cfg.Tailscale.StateDir + "-worker"

	srv := &tsnet.Server{
		Hostname:  hostname + "-worker",
		Ephemeral: cfg.Worker.Ephemeral,
	}
	if cfg.Tailscale.StateDir != "" {
		srv.Dir = stateDir
	}
	if len(cfg.Worker.Tags) > 0 {
		srv.AdvertiseTags = cfg.Worker.Tags
	}
	defer srv.Close()

	if _, err := srv.Up(ctx); err != nil {
		slog.Error("tsnet failed to start", "error", err)
		os.Exit(1)
	}
	slog.Info("tsnet is up", "hostname", hostname+"-worker")

	tsClient := tsapi.NewClient(
		cfg.Tailscale.OAuthClientID,
		cfg.Tailscale.OAuthClientSecret,
		cfg.Tailscale.Tailnet,
	)

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

	w := worker.New(tc, cfg.Temporal.TaskQueue, worker.Options{})

	activities := &grant.Activities{TS: tsClient, Temporal: tc}
	w.RegisterWorkflow(grant.GrantWorkflow)
	w.RegisterWorkflow(grant.ApprovalWorkflow)
	w.RegisterWorkflow(grant.DeviceTagManagerWorkflow)
	w.RegisterWorkflow(grant.ReconciliationWorkflow)
	w.RegisterActivity(activities)

	slog.Info("starting temporal worker", "taskQueue", cfg.Temporal.TaskQueue)

	// Collect all grant tags for reconciliation.
	grantStore, err := grant.NewYAMLGrantTypeStore(cfg.Grants)
	if err != nil {
		slog.Error("failed to create grant store", "error", err)
		os.Exit(1)
	}
	grantTypes, _ := grantStore.List()
	var allGrantTags []string
	seen := make(map[string]struct{})
	for _, gt := range grantTypes {
		for _, tag := range gt.Tags {
			if _, ok := seen[tag]; !ok {
				seen[tag] = struct{}{}
				allGrantTags = append(allGrantTags, tag)
			}
		}
	}

	// Ensure a single ReconciliationWorkflow is running. WorkflowIDReusePolicy
	// prevents duplicates if one already exists from a previous run.
	reconcileOpts := client.StartWorkflowOptions{
		ID:        "reconciliation",
		TaskQueue: cfg.Temporal.TaskQueue,
	}
	reconcileInput := grant.ReconciliationInput{GrantTags: allGrantTags}
	_, err = tc.ExecuteWorkflow(ctx, reconcileOpts, grant.ReconciliationWorkflow, reconcileInput)
	if err != nil {
		slog.Warn("failed to start reconciliation workflow (may already be running)", "error", err)
	} else {
		slog.Info("reconciliation workflow started", "grantTags", allGrantTags)
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		slog.Info("shutting down worker")
		cancel()
		srv.Close()
	}()

	if err := w.Run(worker.InterruptCh()); err != nil {
		slog.Error("worker exited with error", "error", err)
		os.Exit(1)
	}
}
