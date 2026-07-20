package discord

import (
	"context"
	"log/slog"
	"reflect"

	"github.com/bwmarrin/discordgo"
	"github.com/luar/stoatcord/internal/canonical"
	"github.com/luar/stoatcord/internal/engine"
)

// MappingReader resolves a discord id's current mapping row. Needed both to
// build a channel op's DependsOn set from its overwrite roles, and -- once
// dependencies are satisfied -- inside Apply to translate a referenced
// Discord role id to its Stoat role id. Identity translation happens at
// this boundary; it is never baked into internal/canonical's types.
type MappingReader interface {
	Get(entityType, discordID string) (engine.Mapping, error)
}

// ChannelWriter is the subset of *internal/stoat.Client's channel methods
// this package needs, kept narrow per the engine.MappingStore precedent in
// this codebase (define the interface where it's consumed, not where it's
// implemented).
type ChannelWriter interface {
	CreateChannel(ctx context.Context, serverID string, ch canonical.StoatChannel) (string, error)
	EditChannel(ctx context.Context, channelID string, ch canonical.StoatChannel) error
	DeleteChannel(ctx context.Context, channelID string) error
}

// BuildChannelOp translates a Discord channel create/update event into an
// engine.Op. ok is false for channel types this mirror does not model
// (categories, threads, DMs -- see ChannelTypeFromDiscord).
func BuildChannelOp(kind engine.OpKind, ch *discordgo.Channel, stoatServerID string, mappings MappingReader, writer ChannelWriter, logger *slog.Logger) (engine.Op, bool) {
	canonicalCh, ok := ChannelFromDiscord(ch, logger)
	if !ok {
		return engine.Op{}, false
	}

	canonicalState, err := canonicalCh.CanonicalJSON()
	if err != nil {
		logger.Error("discord: failed to serialize channel canonical state", "channel_id", ch.ID, "error", err)
		return engine.Op{}, false
	}

	dependsOn := make([]engine.DependencyKey, 0, len(canonicalCh.Overwrites))
	for roleID := range canonicalCh.Overwrites {
		dependsOn = append(dependsOn, engine.DependencyKey{EntityType: engine.EntityRole, DiscordID: roleID})
	}

	op := engine.Op{
		Kind:           kind,
		EntityType:     engine.EntityChannel,
		DiscordID:      ch.ID,
		CanonicalState: string(canonicalState),
		DependsOn:      dependsOn,
		Diff: func(storedCanonicalState string) (bool, error) {
			stored, err := canonical.ParseChannelCanonicalJSON([]byte(storedCanonicalState))
			if err != nil {
				return false, err
			}
			return reflect.DeepEqual(stored.ToStoat(logger), canonicalCh.ToStoat(logger)), nil
		},
		Apply: func(ctx context.Context) (string, error) {
			stoatCh := canonicalCh.ToStoat(logger)
			resolved, err := resolveOverwriteRoleIDs(stoatCh.Overwrites, mappings)
			if err != nil {
				return "", err
			}
			stoatCh.Overwrites = resolved

			mapping, err := mappings.Get(string(engine.EntityChannel), ch.ID)
			if err != nil {
				return "", err
			}
			// Found alone isn't enough: a pending row (create written but
			// not yet remote-confirmed) has an empty StoatID. Reading it
			// here would send Stoat a PATCH to "/channels/" with no id.
			// The engine now serializes all ops for a given entity, so
			// this should only ever be hit by a stale pending row left
			// over from a crash, not a live in-flight create.
			if mapping.Found && mapping.Status == engine.StatusActive {
				if err := writer.EditChannel(ctx, mapping.StoatID, stoatCh); err != nil {
					return "", err
				}
				return mapping.StoatID, nil
			}
			return writer.CreateChannel(ctx, stoatServerID, stoatCh)
		},
	}
	return op, true
}

// BuildChannelDeleteOp translates a Discord channel delete event into an
// engine.Op. Apply is a no-op if the channel was never mapped (e.g. it was
// a category or otherwise unsupported type).
func BuildChannelDeleteOp(discordChannelID string, mappings MappingReader, writer ChannelWriter) engine.Op {
	return engine.Op{
		Kind:       engine.OpDelete,
		EntityType: engine.EntityChannel,
		DiscordID:  discordChannelID,
		Apply: func(ctx context.Context) (string, error) {
			mapping, err := mappings.Get(string(engine.EntityChannel), discordChannelID)
			if err != nil {
				return "", err
			}
			// See BuildChannelOp: a pending row has no usable StoatID.
			if !mapping.Found || mapping.Status != engine.StatusActive {
				return "", nil
			}
			if err := writer.DeleteChannel(ctx, mapping.StoatID); err != nil {
				return "", err
			}
			return "", nil
		},
	}
}

// resolveOverwriteRoleIDs re-keys a StoatOverwrite map from Discord role ids
// to their mapped Stoat role ids. A role missing from the mapping table is
// skipped defensively -- DependsOn should already have gated the op until
// every referenced role is mapped, so this only guards against a race.
func resolveOverwriteRoleIDs(overwrites map[string]canonical.StoatOverwrite, mappings MappingReader) (map[string]canonical.StoatOverwrite, error) {
	resolved := make(map[string]canonical.StoatOverwrite, len(overwrites))
	for discordRoleID, ow := range overwrites {
		m, err := mappings.Get(string(engine.EntityRole), discordRoleID)
		if err != nil {
			return nil, err
		}
		if !m.Found {
			continue
		}
		resolved[m.StoatID] = ow
	}
	return resolved, nil
}
