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
	"github.com/luar/stoatcord/internal/discord"
	"github.com/luar/stoatcord/internal/engine"
	"github.com/luar/stoatcord/internal/lock"
	logging "github.com/luar/stoatcord/internal/log"
	"github.com/luar/stoatcord/internal/reconcile"
	"github.com/luar/stoatcord/internal/stoat"
	"github.com/luar/stoatcord/internal/store"
)

const (
	defaultEnvFile = ".env"
	dbFileName     = "stoatcord.db"
	lockFileName   = "stoatcord.lock"
	logFileName    = "stoatcord.log"

	// Per-bucket floor spacing between Stoat remote calls
	// (engine.BucketedRateLimiter), source-ground-truthed from Stoat's own
	// rate limiter (crates/core/ratelimits/src/ratelimiter.rs,
	// crates/delta/src/util/ratelimits.rs): each bucket allows N requests
	// per fixed 10s window, keyed by (bucket, resource). Min-interval
	// spacing of 10s/N is a strict, provably-safe approximation of that
	// fixed window; the limiter's existing Backoff (honoring a real
	// Retry-After) is the backstop for clock skew and live+reconcile
	// overlap on the same bucket.
	stoatServersInterval   = 2 * time.Second        // "servers" bucket: 5/10s -- role/category/server metadata writes, one shared bucket for the whole mirror server
	stoatChannelsInterval  = 700 * time.Millisecond // "channels" bucket: 15/10s -- channel structure writes, independent per channel id
	stoatMessagingInterval = 1 * time.Second        // "messaging" bucket: 10/10s -- message sends, independent per channel id
	stoatDefaultInterval   = 500 * time.Millisecond // "any" bucket (fallback): 20/10s

	// shutdownDrainTimeout bounds how long shutdown waits for in-flight
	// engine ops to finish. An op permanently deferred on a dependency that
	// never confirms (a stuck remote failure, a stale pending mapping row)
	// would otherwise block eng.Wait() forever and wedge SIGTERM/SIGINT.
	shutdownDrainTimeout = 10 * time.Second

	// guildReadyTimeout bounds how long startup waits for the mirrored
	// guild's own GUILD_CREATE event. discordSession.Open() returns on
	// READY, before per-guild data arrives -- reading the state cache
	// before this yields empty categories/channels/roles.
	guildReadyTimeout = 30 * time.Second

	// stoatHealthyTimeout bounds how long startup waits for the Stoat
	// gateway to report healthy before running the converge pass.
	// Converging before Stoat is healthy would just enqueue every op to
	// op_queue instead of applying (engine's health gate), which is safe
	// but pointless on a normal fresh start.
	stoatHealthyTimeout = 30 * time.Second
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
	discordSession.Identify.Intents = discordgo.IntentsGuilds | discordgo.IntentsGuildMessages | discordgo.IntentsMessageContent

	mappings := mappingStoreAdapter{st}
	stoatGateway := stoat.NewGateway(logger)
	health := &compositeHealthChecker{
		discordReady: func() bool { return discordSession.DataReady },
		stoatHealthy: stoatGateway.Healthy,
	}
	limiter := engine.NewBucketedRateLimiter(map[string]time.Duration{
		"servers":   stoatServersInterval,
		"channels":  stoatChannelsInterval,
		"messaging": stoatMessagingInterval,
	}, stoatDefaultInterval)
	eng := engine.New(mappings, health, st, limiter, logger)
	eng.DryRun = cfg.DryRun

	discord.RegisterHandlers(discordSession, cfg.DiscordGuild, cfg.StoatServerID, mappings, stoatClient, eng, logger)

	if err := discordSession.Open(); err != nil {
		logger.Error("discord session open failed", "error", err)
		os.Exit(1)
	}
	defer discordSession.Close()
	logger.Info("discord gateway connected")

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	stoatClient.Connect(ctx, stoatGateway)
	logger.Info("stoat gateway connecting", "ws_url", stoatClient.WSURL())

	if err := discord.WaitForGuildReady(ctx, discordSession, cfg.DiscordGuild, guildReadyTimeout); err != nil {
		logger.Error("timed out waiting for discord guild state", "error", err)
		os.Exit(1)
	}
	if err := stoat.WaitForHealthy(ctx, stoatGateway, stoatHealthyTimeout); err != nil {
		logger.Error("timed out waiting for stoat gateway", "error", err)
		os.Exit(1)
	}
	logger.Info("running startup structure reconcile")
	categories, channels, roles := discord.StructureFromState(discordSession, cfg.DiscordGuild, logger)
	reconcileParams := reconcile.Params{
		ServerID:   cfg.StoatServerID,
		GuildID:    cfg.DiscordGuild,
		Categories: categories,
		Channels:   channels,
		Roles:      roles,
		Mappings:   mappings,
		Reader:     stoatClient,
		Writer:     stoatClient,
		DryRun:     cfg.DryRun,
		Logger:     logger,
	}
	if err := reconcile.Bind(ctx, reconcileParams); err != nil {
		logger.Error("startup reconcile: bind pass failed", "error", err)
		os.Exit(1)
	}
	if err := reconcile.ReconcileLive(ctx, reconcileParams); err != nil {
		logger.Error("startup reconcile: live pass failed", "error", err)
		os.Exit(1)
	}
	discord.ConvergeAll(discordSession, cfg.DiscordGuild, cfg.StoatServerID, mappings, stoatClient, eng, logger)
	logger.Info("startup structure reconcile complete")

	<-ctx.Done()
	logger.Info("shutting down")

	if drainEngine(eng.Wait, shutdownDrainTimeout) {
		logger.Warn("engine: shutdown timed out waiting for in-flight ops, exiting anyway", "timeout", shutdownDrainTimeout)
	}
}
