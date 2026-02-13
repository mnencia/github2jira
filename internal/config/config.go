// Package config handles loading and validating the application configuration
// from ~/.config/github2jira/config.yaml.
package config

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/viper"
)

// Config is the top-level application configuration.
type Config struct {
	GitHub GitHubConfig `mapstructure:"github"`
	Jira   JiraConfig   `mapstructure:"jira"`
}

// GitHubConfig holds GitHub API credentials.
type GitHubConfig struct {
	Token string `mapstructure:"token"`
}

// JiraConfig holds JIRA connection details, project defaults, workflow
// transition names, and optional GitHub-to-JIRA user mappings.
type JiraConfig struct {
	URL       string            `mapstructure:"url"`
	User      string            `mapstructure:"user"`
	Token     string            `mapstructure:"token"`
	Project   string            `mapstructure:"project"`
	Component string            `mapstructure:"component"`
	Statuses  StatusesConfig    `mapstructure:"statuses"`
	Users     map[string]string `mapstructure:"users"`
}

// StatusesConfig maps PR state to a JIRA workflow transition name.
type StatusesConfig struct {
	WithPR    string `mapstructure:"with_pr"`
	WithoutPR string `mapstructure:"without_pr"`
	MergedPR  string `mapstructure:"merged_pr"`
	Abandoned string `mapstructure:"abandoned"`
}

// Load reads the configuration file from configDir/github2jira/config.yaml,
// applies defaults, and validates that all required fields are present.
func Load(configDir string) (*Config, error) {
	v := viper.New()
	v.SetConfigName("config")
	v.SetConfigType("yaml")
	v.AddConfigPath(filepath.Join(configDir, "github2jira"))

	v.SetDefault("jira.statuses.with_pr", "In Development")
	v.SetDefault("jira.statuses.without_pr", "Ready")
	v.SetDefault("jira.statuses.merged_pr", "Done")
	v.SetDefault("jira.statuses.abandoned", "Abandoned")

	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func (c *Config) validate() error {
	if c.GitHub.Token == "" {
		return fmt.Errorf("github.token is required")
	}
	if c.Jira.URL == "" {
		return fmt.Errorf("jira.url is required")
	}
	if c.Jira.User == "" {
		return fmt.Errorf("jira.user is required")
	}
	if c.Jira.Token == "" {
		return fmt.Errorf("jira.token is required")
	}
	if c.Jira.Project == "" {
		return fmt.Errorf("jira.project is required")
	}
	if c.Jira.Component == "" {
		return fmt.Errorf("jira.component is required")
	}
	return nil
}
