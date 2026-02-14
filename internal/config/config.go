package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Temporal  TemporalConfig   `yaml:"temporal"`
	Tailscale TailscaleConfig  `yaml:"tailscale"`
	Server    ServerConfig     `yaml:"server"`
	Worker    WorkerConfig     `yaml:"worker"`
	Grants    []GrantTypeConfig `yaml:"grants"`
}

type TemporalConfig struct {
	Address   string `yaml:"address"`
	Namespace string `yaml:"namespace"`
	TaskQueue string `yaml:"taskQueue"`
	UseTsnet  bool   `yaml:"useTsnet"`
}

type TailscaleConfig struct {
	Hostname          string `yaml:"hostname"`
	StateDir          string `yaml:"stateDir"`
	OAuthClientID     string `yaml:"oauthClientID"`
	OAuthClientSecret string `yaml:"oauthClientSecret"`
	Tailnet           string `yaml:"tailnet"`
}

type ServerConfig struct {
	ListenAddr string         `yaml:"listenAddr"`
	UseTLS     *bool          `yaml:"useTLS"`
	Tags       []string       `yaml:"tags"`
	Service    *ServiceConfig `yaml:"service"`
}

type ServiceConfig struct {
	Name    string   `yaml:"name"`    // VIP service name, e.g. "svc:tailgrant"
	Port    uint16   `yaml:"port"`    // Port to advertise (e.g. 443)
	HTTPS   bool     `yaml:"https"`   // Use HTTPS mode (vs raw TCP)
	Comment string   `yaml:"comment"` // Optional description
	Tags    []string `yaml:"tags"`    // ACL tags for the VIP service
}

type WorkerConfig struct {
	Ephemeral bool     `yaml:"ephemeral"`
	Tags      []string `yaml:"tags"`
}

type GrantTypeConfig struct {
	Name              string                   `yaml:"name"`
	Description       string                   `yaml:"description"`
	Tags              []string                 `yaml:"tags"`
	PostureAttributes []PostureAttributeConfig `yaml:"postureAttributes"`
	MaxDuration       string                   `yaml:"maxDuration"`
	RiskLevel         string                   `yaml:"riskLevel"`
	Approvers         []string                 `yaml:"approvers"`
	Action            string                   `yaml:"action"`
	UserAction        *UserActionConfig        `yaml:"userAction"`
}

type PostureAttributeConfig struct {
	Key    string `yaml:"key"`
	Value  any    `yaml:"value"`
	Target string `yaml:"target"` // "requester" or "target", defaults to "requester"
}

type UserActionConfig struct {
	Role string `yaml:"role"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config %s: %w", path, err)
	}

	cfg := &Config{}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config %s: %w", path, err)
	}

	applyDefaults(cfg)
	applyEnvOverrides(cfg)

	return cfg, nil
}

func LoadFromEnv() (*Config, error) {
	path := os.Getenv("CONFIG_PATH")
	if path == "" {
		return nil, fmt.Errorf("CONFIG_PATH environment variable not set")
	}
	return Load(path)
}

func applyDefaults(cfg *Config) {
	if cfg.Temporal.Namespace == "" {
		cfg.Temporal.Namespace = "default"
	}
	if cfg.Temporal.TaskQueue == "" {
		cfg.Temporal.TaskQueue = "tailgrant"
	}
	if cfg.Server.ListenAddr == "" {
		cfg.Server.ListenAddr = ":80"
	}
	if cfg.Server.UseTLS == nil {
		f := false
		cfg.Server.UseTLS = &f
	}
}

func applyEnvOverrides(cfg *Config) {
	if id := os.Getenv("TS_OAUTH_CLIENT_ID"); id != "" {
		cfg.Tailscale.OAuthClientID = id
	}
	if secret := os.Getenv("TS_OAUTH_CLIENT_SECRET"); secret != "" {
		cfg.Tailscale.OAuthClientSecret = secret
	}
	if tailnet := os.Getenv("TS_TAILNET"); tailnet != "" {
		cfg.Tailscale.Tailnet = tailnet
	}
}
