package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeEnvFile(t *testing.T, dir, contents string) string {
	t.Helper()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("os.WriteFile: %v", err)
	}
	return path
}

func TestDataDir_UsesXDGDataHomeWhenSet(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "/xdg-data")

	dir, err := DataDir()
	if err != nil {
		t.Fatalf("DataDir: %v", err)
	}
	want := filepath.Join("/xdg-data", "stoatcord")
	if dir != want {
		t.Errorf("DataDir() = %q, want %q", dir, want)
	}
}

func TestDataDir_FallsBackToHomeLocalShare(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("HOME", "/home/testuser")

	dir, err := DataDir()
	if err != nil {
		t.Fatalf("DataDir: %v", err)
	}
	want := filepath.Join("/home/testuser", ".local", "share", "stoatcord")
	if dir != want {
		t.Errorf("DataDir() = %q, want %q", dir, want)
	}
}

func TestLoad_MissingRequiredFields_FailsLoud(t *testing.T) {
	dir := t.TempDir()
	path := writeEnvFile(t, dir, `STOATCORD_DISCORD_TOKEN=d-token`)

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for missing required fields, got nil")
	}
}

func TestLoad_AllRequiredFields_Succeeds(t *testing.T) {
	dir := t.TempDir()
	path := writeEnvFile(t, dir, `
STOATCORD_DISCORD_TOKEN=d-token
STOATCORD_STOAT_TOKEN=s-token
DISCORD_SERVER_ID=guild-1
STOAT_SERVER_ID=server-1
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.DiscordToken != "d-token" {
		t.Errorf("DiscordToken = %q, want %q", cfg.DiscordToken, "d-token")
	}
	if cfg.StoatToken != "s-token" {
		t.Errorf("StoatToken = %q, want %q", cfg.StoatToken, "s-token")
	}
	if cfg.DiscordGuild != "guild-1" {
		t.Errorf("DiscordGuild = %q, want %q", cfg.DiscordGuild, "guild-1")
	}
	if cfg.StoatServerID != "server-1" {
		t.Errorf("StoatServerID = %q, want %q", cfg.StoatServerID, "server-1")
	}
}

func TestLoad_RealEnvOverridesEnvFile(t *testing.T) {
	dir := t.TempDir()
	path := writeEnvFile(t, dir, `
STOATCORD_DISCORD_TOKEN=file-token
STOATCORD_STOAT_TOKEN=s-token
DISCORD_SERVER_ID=guild-1
STOAT_SERVER_ID=server-1
`)

	t.Setenv("STOATCORD_DISCORD_TOKEN", "env-token")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.DiscordToken != "env-token" {
		t.Errorf("DiscordToken = %q, want real-env override %q", cfg.DiscordToken, "env-token")
	}
}

func TestLoad_LogLevel_OptionalDefaultsEmpty(t *testing.T) {
	dir := t.TempDir()
	path := writeEnvFile(t, dir, `
STOATCORD_DISCORD_TOKEN=d-token
STOATCORD_STOAT_TOKEN=s-token
DISCORD_SERVER_ID=guild-1
STOAT_SERVER_ID=server-1
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.LogLevel != "" {
		t.Errorf("LogLevel = %q, want empty when unset", cfg.LogLevel)
	}
}

func TestLoad_LogLevel_ReadFromEnv(t *testing.T) {
	dir := t.TempDir()
	path := writeEnvFile(t, dir, `
STOATCORD_DISCORD_TOKEN=d-token
STOATCORD_STOAT_TOKEN=s-token
DISCORD_SERVER_ID=guild-1
STOAT_SERVER_ID=server-1
LOG_LEVEL=debug
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("LogLevel = %q, want %q", cfg.LogLevel, "debug")
	}
}

func TestLoad_StoatAPIBase_DefaultsWhenUnset(t *testing.T) {
	dir := t.TempDir()
	path := writeEnvFile(t, dir, `
STOATCORD_DISCORD_TOKEN=d-token
STOATCORD_STOAT_TOKEN=s-token
DISCORD_SERVER_ID=guild-1
STOAT_SERVER_ID=server-1
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.StoatAPIBase != "https://api.revolt.chat" {
		t.Errorf("StoatAPIBase = %q, want default https://api.revolt.chat", cfg.StoatAPIBase)
	}
}

func TestLoad_StoatAPIBase_ReadFromEnv(t *testing.T) {
	dir := t.TempDir()
	path := writeEnvFile(t, dir, `
STOATCORD_DISCORD_TOKEN=d-token
STOATCORD_STOAT_TOKEN=s-token
DISCORD_SERVER_ID=guild-1
STOAT_SERVER_ID=server-1
STOAT_API_BASE=https://custom.example.com
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.StoatAPIBase != "https://custom.example.com" {
		t.Errorf("StoatAPIBase = %q, want https://custom.example.com", cfg.StoatAPIBase)
	}
}

func TestLoad_MissingEnvFile_FallsBackToRealEnv(t *testing.T) {
	t.Setenv("STOATCORD_DISCORD_TOKEN", "env-token")
	t.Setenv("STOATCORD_STOAT_TOKEN", "env-stoat-token")
	t.Setenv("DISCORD_SERVER_ID", "env-guild")
	t.Setenv("STOAT_SERVER_ID", "env-server")

	cfg, err := Load("/nonexistent/path/.env")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.DiscordToken != "env-token" {
		t.Errorf("DiscordToken = %q, want %q", cfg.DiscordToken, "env-token")
	}
}
