package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

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

	tsClient := tsapi.NewClient(
		cfg.Tailscale.OAuthClientID,
		cfg.Tailscale.OAuthClientSecret,
		cfg.Tailscale.Tailnet,
	)

	stateDir := cfg.Tailscale.StateDir + "-worker"

	srv := &tsnet.Server{
		Hostname:     hostname + "-worker",
		Ephemeral:    cfg.Worker.Ephemeral,
		ClientSecret: fmt.Sprintf("%s?ephemeral=%t&preauthorized=true", cfg.Tailscale.OAuthClientSecret, cfg.Worker.Ephemeral),
	}
	if cfg.Tailscale.StateDir != "" {
		srv.Dir = stateDir
	}
	if len(cfg.Worker.Tags) > 0 {
		srv.AdvertiseTags = cfg.Worker.Tags
	}
	defer func() { _ = srv.Close() }()

	if _, err := srv.Up(ctx); err != nil {
		slog.Error("tsnet failed to start", "error", err)
		os.Exit(1)
	}
	slog.Info("tsnet is up", "hostname", hostname+"-worker")

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

	w := worker.New(tc, cfg.Temporal.TaskQueue, worker.Options{})

	userOps := tsapi.NewUserOperations(tsClient)
	activities := &grant.Activities{TS: tsClient, Temporal: tc, UserOps: userOps}
	w.RegisterWorkflow(grant.GrantWorkflow)
	w.RegisterWorkflow(grant.ApprovalWorkflow)
	w.RegisterWorkflow(grant.DeviceTagManagerWorkflow)
	w.RegisterWorkflow(grant.ReconciliationWorkflow)
	w.RegisterActivity(activities)

	slog.Info("starting temporal worker", "taskQueue", cfg.Temporal.TaskQueue)

	// Collect all grant tags for reconciliation (skip user-action grant types
	// since they don't manage device tags).
	grantStore, err := grant.NewYAMLGrantTypeStore(cfg.Grants)
	if err != nil {
		slog.Error("failed to create grant store", "error", err)
		os.Exit(1)
	}
	grantTypes, _ := grantStore.List()
	var allGrantTags []string
	var allPostureKeys []string
	seenTags := make(map[string]struct{})
	seenKeys := make(map[string]struct{})
	for _, gt := range grantTypes {
		if gt.Action == grant.ActionUserRole || gt.Action == grant.ActionUserRestore {
			continue
		}
		for _, tag := range gt.Tags {
			if _, ok := seenTags[tag]; !ok {
				seenTags[tag] = struct{}{}
				allGrantTags = append(allGrantTags, tag)
			}
		}
		for _, pa := range gt.PostureAttributes {
			if _, ok := seenKeys[pa.Key]; !ok {
				seenKeys[pa.Key] = struct{}{}
				allPostureKeys = append(allPostureKeys, pa.Key)
			}
		}
	}

	// Ensure a single ReconciliationWorkflow is running. WorkflowIDReusePolicy
	// prevents duplicates if one already exists from a previous run.
	reconcileOpts := client.StartWorkflowOptions{
		ID:        "reconciliation",
		TaskQueue: cfg.Temporal.TaskQueue,
	}
	reconcileInput := grant.ReconciliationInput{
		GrantTags:        allGrantTags,
		GrantPostureKeys: allPostureKeys,
	}
	_, err = tc.ExecuteWorkflow(ctx, reconcileOpts, grant.ReconciliationWorkflow, reconcileInput)
	if err != nil {
		slog.Warn("failed to start reconciliation workflow (may already be running)", "error", err)
	} else {
		slog.Info("reconciliation workflow started", "grantTags", allGrantTags, "postureKeys", allPostureKeys)
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		slog.Info("shutting down worker")
		cancel()
		_ = srv.Close()
	}()

	if err := w.Run(worker.InterruptCh()); err != nil {
		slog.Error("worker exited with error", "error", err)
		os.Exit(1)
	}
}
