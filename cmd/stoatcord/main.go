package main

import (
	"context"
	"errors"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/bwmarrin/discordgo"
	"golang.org/x/term"

	"github.com/luar/stoatcord/internal/config"
	"github.com/luar/stoatcord/internal/engine"
	"github.com/luar/stoatcord/internal/lock"
	logging "github.com/luar/stoatcord/internal/log"
	"github.com/luar/stoatcord/internal/stoat"
	"github.com/luar/stoatcord/internal/store"
)

const (
	defaultEnvFile = ".env"
	dbFileName     = "stoatcord.db"
	lockFileName   = "stoatcord.lock"
	logFileName    = "stoatcord.log"

	// stoatMinRemoteInterval is the floor spacing between Stoat remote
	// calls (engine.GlobalRateLimiter). Not a spec-known constant -- Phase 0
	// didn't observe Stoat's exact rate limit -- so it is conservative.
	stoatMinRemoteInterval = 250 * time.Millisecond

	// shutdownDrainTimeout bounds how long shutdown waits for in-flight
	// engine ops to finish. An op permanently deferred on a dependency that
	// never confirms (a stuck remote failure, a stale pending mapping row)
	// would otherwise block eng.Wait() forever and wedge SIGTERM/SIGINT.
	shutdownDrainTimeout = 10 * time.Second
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

	dataDir, err := config.DataDir()
	if err != nil {
		os.Stderr.WriteString("resolve data dir failed: " + err.Error() + "\n")
		os.Exit(1)
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		os.Stderr.WriteString("create data dir failed: " + err.Error() + "\n")
		os.Exit(1)
	}

	// Logs also append to a file in dataDir (same place as the DB, already
	// outside the repo -- no .gitignore entry needed) so a run's output
	// survives after the terminal is gone, for post-mortem grepping.
	logPath := filepath.Join(dataDir, logFileName)
	logFile, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		os.Stderr.WriteString("open log file failed: " + err.Error() + "\n")
		os.Exit(1)
	}
	defer logFile.Close()
	logger := logging.New(io.MultiWriter(os.Stderr, logFile), level, term.IsTerminal(int(os.Stderr.Fd())))

	logger.Info("config loaded", "discord_guild", cfg.DiscordGuild, "stoat_server", cfg.StoatServerID)
	logger.Info("logging to file", "path", logPath)
	if cfg.DryRun {
		logger.Warn("engine dry-run is ON -- no real Stoat writes will happen (set STOATCORD_DRY_RUN=false to disable)")
	}

	daemonLock, err := lock.Acquire(filepath.Join(dataDir, lockFileName))
	if err != nil {
		if errors.Is(err, lock.ErrLocked) {
			logger.Error("another stoatcord instance is already running", "path", filepath.Join(dataDir, lockFileName))
		} else {
			logger.Error("acquire daemon lock failed", "error", err)
		}
		os.Exit(1)
	}
	defer daemonLock.Release()

	dbPath := filepath.Join(dataDir, dbFileName)

	st, err := store.Open(dbPath)
	if err != nil {
		logger.Error("store open failed", "error", err)
		os.Exit(1)
	}
	defer st.Close()
	logger.Info("store opened and migrations applied", "path", dbPath)

	stoatClient, err := stoat.New(cfg.StoatToken, cfg.StoatAPIBase)
	if err != nil {
		logger.Error("stoat client construction failed", "error", err)
		os.Exit(1)
	}

	discordSession, err := discordgo.New("Bot " + cfg.DiscordToken)
	if err != nil {
		logger.Error("discord session construction failed", "error", err)
		os.Exit(1)
	}
	discordSession.Identify.Intents = discordgo.IntentsGuilds

	mappings := mappingStoreAdapter{st}
	health := &compositeHealthChecker{discordSession: discordSession, stoatGateway: stoat.NewGateway(logger)}
	limiter := engine.NewGlobalRateLimiter(stoatMinRemoteInterval)
	eng := engine.New(mappings, health, st, limiter, logger)
	eng.DryRun = cfg.DryRun

	registerDiscordHandlers(discordSession, cfg.DiscordGuild, cfg.StoatServerID, mappings, stoatClient, eng, logger)

	if err := discordSession.Open(); err != nil {
		logger.Error("discord session open failed", "error", err)
		os.Exit(1)
	}
	defer discordSession.Close()
	logger.Info("discord gateway connected")

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	stoatClient.Connect(ctx, health.stoatGateway)
	logger.Info("stoat gateway connecting", "ws_url", stoatClient.WSURL())

	if err := waitForGuildReady(ctx, discordSession, cfg.DiscordGuild, guildReadyTimeout); err != nil {
		logger.Error("timed out waiting for discord guild state", "error", err)
		os.Exit(1)
	}
	if err := waitForStoatHealthy(ctx, health.stoatGateway, stoatHealthyTimeout); err != nil {
		logger.Error("timed out waiting for stoat gateway", "error", err)
		os.Exit(1)
	}
	logger.Info("running startup structure reconcile")
	if err := runStartupReconcile(ctx, discordSession, cfg.DiscordGuild, cfg.StoatServerID, mappings, stoatClient, mappings, stoatClient, eng, logger); err != nil {
		logger.Error("startup reconcile failed", "error", err)
		os.Exit(1)
	}
	logger.Info("startup structure reconcile complete")

	<-ctx.Done()
	logger.Info("shutting down")

	if drainEngine(eng.Wait, shutdownDrainTimeout) {
		logger.Warn("engine: shutdown timed out waiting for in-flight ops, exiting anyway", "timeout", shutdownDrainTimeout)
	}
}
