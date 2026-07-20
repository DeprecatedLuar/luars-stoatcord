package main

import (
	"os"
	"path/filepath"

	"golang.org/x/term"

	"github.com/luar/stoatcord/internal/config"
	logging "github.com/luar/stoatcord/internal/log"
	"github.com/luar/stoatcord/internal/store"
)

const (
	defaultEnvFile = ".env"
	dbFileName     = "stoatcord.db"
)

func main() {
	cfg, err := config.Load(defaultEnvFile)
	if err != nil {
		// No logger yet -- config failed before we know the requested level.
		os.Stderr.WriteString("config load failed: " + err.Error() + "\n")
		os.Exit(1)
	}

	level, err := logging.ParseLevel(cfg.LogLevel)
	if err != nil {
		os.Stderr.WriteString(err.Error() + "\n")
		os.Exit(1)
	}
	logger := logging.New(os.Stderr, level, term.IsTerminal(int(os.Stderr.Fd())))

	logger.Info("config loaded", "discord_guild", cfg.DiscordGuild, "stoat_server", cfg.StoatServerID)

	dataDir, err := config.DataDir()
	if err != nil {
		logger.Error("resolve data dir failed", "error", err)
		os.Exit(1)
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		logger.Error("create data dir failed", "error", err, "path", dataDir)
		os.Exit(1)
	}
	dbPath := filepath.Join(dataDir, dbFileName)

	st, err := store.Open(dbPath)
	if err != nil {
		logger.Error("store open failed", "error", err)
		os.Exit(1)
	}
	defer st.Close()
	logger.Info("store opened and migrations applied", "path", dbPath)
}
