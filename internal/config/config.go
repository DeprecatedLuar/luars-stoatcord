package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/joho/godotenv"
)

const appDirName = "stoatcord"

// defaultStoatAPIBase is the official Revolt/Stoat REST endpoint, used when
// STOAT_API_BASE is unset -- this instance's REST API is served here even
// though its gateway is the non-official wss://events.stoat.chat (see
// internal/stoat gap 1).
const defaultStoatAPIBase = "https://api.revolt.chat"

// Config holds the credentials and target identifiers stoatcord needs to run.
type Config struct {
	DiscordToken  string
	StoatToken    string
	DiscordGuild  string
	StoatServerID string
	StoatAPIBase  string
	LogLevel      string
}

const (
	envDiscordToken  = "STOATCORD_DISCORD_TOKEN"
	envStoatToken    = "STOATCORD_STOAT_TOKEN"
	envDiscordGuild  = "DISCORD_SERVER_ID"
	envStoatServerID = "STOAT_SERVER_ID"
	envStoatAPIBase  = "STOAT_API_BASE"
	envLogLevel      = "LOG_LEVEL"
)

// Load reads envFile into the process environment (if it exists; real
// environment variables always take precedence over it), then builds a
// Config from environment variables. It fails loud if any required value
// is still missing.
func Load(envFile string) (*Config, error) {
	if envFile != "" {
		if err := godotenv.Load(envFile); err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("config: load %s: %w", envFile, err)
		}
	}

	cfg := &Config{
		DiscordToken:  os.Getenv(envDiscordToken),
		StoatToken:    os.Getenv(envStoatToken),
		DiscordGuild:  os.Getenv(envDiscordGuild),
		StoatServerID: os.Getenv(envStoatServerID),
		StoatAPIBase:  os.Getenv(envStoatAPIBase),
		LogLevel:      os.Getenv(envLogLevel),
	}
	if cfg.StoatAPIBase == "" {
		cfg.StoatAPIBase = defaultStoatAPIBase
	}

	var missing []string
	if cfg.DiscordToken == "" {
		missing = append(missing, envDiscordToken)
	}
	if cfg.StoatToken == "" {
		missing = append(missing, envStoatToken)
	}
	if cfg.DiscordGuild == "" {
		missing = append(missing, envDiscordGuild)
	}
	if cfg.StoatServerID == "" {
		missing = append(missing, envStoatServerID)
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("config: missing required environment variables: %v", missing)
	}

	return cfg, nil
}

// DataDir resolves the XDG data directory for stoatcord: $XDG_DATA_HOME/stoatcord
// if set, else $HOME/.local/share/stoatcord. It does not create the directory.
func DataDir() (string, error) {
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, appDirName), nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("config: resolve home directory: %w", err)
	}
	return filepath.Join(home, ".local", "share", appDirName), nil
}
