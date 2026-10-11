package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/vmartinv/opensnitch-zenity/internal/rules"
)

// opensnitchd gives up on AskRule after 120s; both dialogs share the
// timeout, and the rest is margin for zenity start-up and the reply.
const maxPromptTimeout = 110 * time.Second

type Config struct {
	Socket             string        `toml:"socket"`
	PromptTimeout      time.Duration `toml:"prompt_timeout"`
	DefaultAction      string        `toml:"default_action"`
	NotifyOnDefault    bool          `toml:"notify_on_default"`
	LogLevel           string        `toml:"log_level"`
	ZenityPath         string        `toml:"zenity_path"`
	LockedCheckCommand string        `toml:"locked_check_command"`
}

func Default() Config {
	return Config{
		Socket:        "unix:///tmp/osui.sock",
		PromptTimeout: maxPromptTimeout,
		DefaultAction: rules.ActionDeny,
		LogLevel:      "info",
		ZenityPath:    "zenity",
	}
}

// DefaultPath is $XDG_CONFIG_HOME/opensnitch-zenity/config.toml.
func DefaultPath() string {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		dir = filepath.Join(os.Getenv("HOME"), ".config")
	}
	return filepath.Join(dir, "opensnitch-zenity", "config.toml")
}

// Load reads path over the defaults. A missing file is not an error unless
// the path was given explicitly.
func Load(path string, explicit bool) (Config, error) {
	cfg := Default()
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		if !explicit && os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, fmt.Errorf("reading %s: %w", path, err)
	}
	return cfg, nil
}

// Validate checks values and normalizes default_action.
func (c *Config) Validate() error {
	if !strings.HasPrefix(c.Socket, "unix://") {
		return fmt.Errorf("socket must be unix:///path, got %q", c.Socket)
	}
	c.DefaultAction = strings.ToLower(c.DefaultAction)
	if !rules.ValidAction(c.DefaultAction) {
		return fmt.Errorf("default_action must be one of %v, got %q", rules.Actions, c.DefaultAction)
	}
	switch c.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("log_level must be debug, info, warn or error, got %q", c.LogLevel)
	}
	if c.PromptTimeout < time.Second || c.PromptTimeout > maxPromptTimeout {
		return fmt.Errorf("prompt_timeout must be between 1s and %s, got %s", maxPromptTimeout, c.PromptTimeout)
	}
	return nil
}
