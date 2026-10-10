package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func write(t *testing.T, content string) string {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDefaultsAreValid(t *testing.T) {
	cfg := Default()
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestLoadFile(t *testing.T) {
	path := write(t, `
socket = "unix:///run/user/1000/osui.sock"
prompt_timeout = "45s"
default_action = "Reject"
notify_on_default = true
`)
	cfg, err := Load(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	if cfg.PromptTimeout != 45*time.Second || cfg.DefaultAction != "reject" || !cfg.NotifyOnDefault ||
		cfg.Socket != "unix:///run/user/1000/osui.sock" {
		t.Errorf("cfg = %+v", cfg)
	}
	if cfg.ZenityPath != "zenity" {
		t.Error("unset keys should keep defaults")
	}
}

func TestMissingFile(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope.toml")
	if _, err := Load(missing, false); err != nil {
		t.Errorf("implicit missing config should be fine: %v", err)
	}
	if _, err := Load(missing, true); err == nil {
		t.Error("explicit missing config should fail")
	}
}

func TestMalformedFile(t *testing.T) {
	if _, err := Load(write(t, "socket = "), false); err == nil {
		t.Error("expected parse error")
	}
	if _, err := Load(write(t, "bogus_key = 1\nsocket = 3"), false); err == nil {
		t.Error("expected type error")
	}
}

func TestValidate(t *testing.T) {
	tests := map[string]func(*Config){
		"tcp socket":                  func(c *Config) { c.Socket = "127.0.0.1:50051" },
		"action":                      func(c *Config) { c.DefaultAction = "accept" },
		"log level":                   func(c *Config) { c.LogLevel = "verbose" },
		"timeout too low":             func(c *Config) { c.PromptTimeout = 0 },
		"timeout beyond daemon limit": func(c *Config) { c.PromptTimeout = time.Minute },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			cfg := Default()
			mutate(&cfg)
			if err := cfg.Validate(); err == nil {
				t.Error("expected error")
			}
		})
	}
}
