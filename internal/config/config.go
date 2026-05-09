package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type EndpointConfig struct {
	URL         string  `yaml:"url"`
	APIKey      string  `yaml:"api_key"`
	Model       string  `yaml:"model"`
	MaxTokens   int     `yaml:"max_tokens"`
	Temperature float64 `yaml:"temperature"`
}

type ServerConfig struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
}

type StorageConfig struct {
	Path string `yaml:"path"`
}

type WhitelistEntry struct {
	Tool           string `yaml:"tool"`
	Allow          string `yaml:"allow"`
	CommandPattern string `yaml:"command_pattern,omitempty"`
}

type ApprovalConfig struct {
	DefaultTimeout int              `yaml:"default_timeout"`
	AutoApproveAll bool             `yaml:"auto_approve_all"`
	Whitelist      []WhitelistEntry `yaml:"whitelist"`
}

type MCPServer struct {
	Name      string   `yaml:"name"`
	Transport string   `yaml:"transport"` // "stdio" or "sse"
	Command   string   `yaml:"command"`
	Args      []string `yaml:"args"`
	URL       string   `yaml:"url"`
}

type Config struct {
	Endpoint     EndpointConfig `yaml:"endpoint"`
	Server       ServerConfig   `yaml:"server"`
	Storage      StorageConfig  `yaml:"storage"`
	Approval     ApprovalConfig `yaml:"approval"`
	MCPServers   []MCPServer    `yaml:"mcp_servers"`
	SystemPrompt string         `yaml:"system_prompt"`
}

func Load(path string) (*Config, error) {
	var err error
	path, err = expandHome(path)
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	cfg.Storage.Path, err = expandHome(cfg.Storage.Path)
	if err != nil {
		return nil, err
	}

	if cfg.Server.Host == "" {
		cfg.Server.Host = "127.0.0.1"
	}
	if cfg.Server.Port == 0 {
		cfg.Server.Port = 3000
	}

	return &cfg, nil
}

func Save(path string, cfg *Config) error {
	var err error
	path, err = expandHome(path)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("creating config dir: %w", err)
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}
	return nil
}

func expandHome(path string) (string, error) {
	if !strings.HasPrefix(path, "~") {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot resolve ~: %w", err)
	}
	return filepath.Join(home, path[1:]), nil
}
