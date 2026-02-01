package config

import (
	"os"
	"testing"
)

func TestLoad(t *testing.T) {
	// Save original env and restore after test
	originalAllowlist := os.Getenv("REPO_ALLOWLIST")
	defer os.Setenv("REPO_ALLOWLIST", originalAllowlist)

	tests := []struct {
		name        string
		envVars     map[string]string
		wantErr     bool
		checkConfig func(*testing.T, *Config)
	}{
		{
			name: "default values with required allowlist",
			envVars: map[string]string{
				"REPO_ALLOWLIST": "owner/repo",
			},
			wantErr: false,
			checkConfig: func(t *testing.T, cfg *Config) {
				if cfg.Port != 8080 {
					t.Errorf("Port = %d, want 8080", cfg.Port)
				}
				if cfg.MetricsPort != 9090 {
					t.Errorf("MetricsPort = %d, want 9090", cfg.MetricsPort)
				}
				if cfg.WorkerCount != 1 {
					t.Errorf("WorkerCount = %d, want 1", cfg.WorkerCount)
				}
				if cfg.QueueSize != 100 {
					t.Errorf("QueueSize = %d, want 100", cfg.QueueSize)
				}
			},
		},
		{
			name: "custom values",
			envVars: map[string]string{
				"PORT":           "9000",
				"METRICS_PORT":   "9091",
				"WORKER_COUNT":   "5",
				"QUEUE_SIZE":     "200",
				"REPO_ALLOWLIST": "myorg/*,special/repo",
			},
			wantErr: false,
			checkConfig: func(t *testing.T, cfg *Config) {
				if cfg.Port != 9000 {
					t.Errorf("Port = %d, want 9000", cfg.Port)
				}
				if cfg.WorkerCount != 5 {
					t.Errorf("WorkerCount = %d, want 5", cfg.WorkerCount)
				}
				if len(cfg.RepoAllowlist) != 2 {
					t.Errorf("RepoAllowlist length = %d, want 2", len(cfg.RepoAllowlist))
				}
			},
		},
		{
			name:    "missing allowlist",
			envVars: map[string]string{},
			wantErr: true,
		},
		{
			name: "empty allowlist",
			envVars: map[string]string{
				"REPO_ALLOWLIST": "   ",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear and set environment variables
			os.Unsetenv("PORT")
			os.Unsetenv("METRICS_PORT")
			os.Unsetenv("WORKER_COUNT")
			os.Unsetenv("QUEUE_SIZE")
			os.Unsetenv("REPO_ALLOWLIST")

			for key, value := range tt.envVars {
				os.Setenv(key, value)
			}

			cfg, err := Load()
			if (err != nil) != tt.wantErr {
				t.Errorf("Load() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && tt.checkConfig != nil {
				tt.checkConfig(t, cfg)
			}
		})
	}
}

func TestIsRepoAllowed(t *testing.T) {
	cfg := &Config{
		RepoAllowlist: []string{"myorg/*", "special/repo", "exact/match"},
	}

	tests := []struct {
		repo    string
		allowed bool
	}{
		{"myorg/repo1", true},
		{"myorg/repo2", true},
		{"MYORG/repo3", true}, // case insensitive
		{"special/repo", true},
		{"Special/Repo", true}, // case insensitive
		{"exact/match", true},
		{"other/repo", false},
		{"myorg", false}, // not a full repo path
		{"notmyorg/repo", false},
	}

	for _, tt := range tests {
		t.Run(tt.repo, func(t *testing.T) {
			got := cfg.IsRepoAllowed(tt.repo)
			if got != tt.allowed {
				t.Errorf("IsRepoAllowed(%q) = %v, want %v", tt.repo, got, tt.allowed)
			}
		})
	}
}

func TestMatchPattern(t *testing.T) {
	tests := []struct {
		pattern string
		repo    string
		match   bool
	}{
		{"owner/repo", "owner/repo", true},
		{"owner/repo", "Owner/Repo", true}, // case insensitive
		{"owner/*", "owner/repo1", true},
		{"owner/*", "owner/repo2", true},
		{"owner/*", "other/repo", false},
		{"*/*", "any/repo", true},
		{"*/*", "another/project", true},
		{"myorg/*", "myorg/test", true},
		{"myorg/*", "notmyorg/test", false},
	}

	for _, tt := range tests {
		t.Run(tt.pattern+"_"+tt.repo, func(t *testing.T) {
			got := matchPattern(tt.pattern, tt.repo)
			if got != tt.match {
				t.Errorf("matchPattern(%q, %q) = %v, want %v", tt.pattern, tt.repo, got, tt.match)
			}
		})
	}
}

func TestParseAllowlist(t *testing.T) {
	tests := []struct {
		input string
		want  []string
	}{
		{"owner/repo", []string{"owner/repo"}},
		{"owner/repo1,owner/repo2", []string{"owner/repo1", "owner/repo2"}},
		{"owner/repo1, owner/repo2, owner/repo3", []string{"owner/repo1", "owner/repo2", "owner/repo3"}},
		{"  owner/repo  ", []string{"owner/repo"}},
		{"owner/repo1,,owner/repo2", []string{"owner/repo1", "owner/repo2"}}, // empty entries ignored
		{"", []string{}},
		{"   ", []string{}},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := parseAllowlist(tt.input)
			if len(got) != len(tt.want) {
				t.Errorf("parseAllowlist(%q) length = %d, want %d", tt.input, len(got), len(tt.want))
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("parseAllowlist(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.want[i])
				}
			}
		})
	}
}
