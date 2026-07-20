package discord

import (
	"context"
	"log/slog"
	"reflect"

	"github.com/bwmarrin/discordgo"
	"github.com/luar/stoatcord/internal/canonical"
	"github.com/luar/stoatcord/internal/engine"
)

// everyoneOverwriteRoleID is the sentinel Stoat expects in place of a role
// id when a channel permission overwrite targets Discord's @everyone role
// (mirrors internal/stoat/permission.go's defaultRoleID, which the two
// packages can't share -- internal/stoat's Stoat-shape constant is not this
// package's concern).
const everyoneOverwriteRoleID = "default"

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
func BuildChannelOp(kind engine.OpKind, ch *discordgo.Channel, guildID, stoatServerID string, mappings MappingReader, writer ChannelWriter, logger *slog.Logger) (engine.Op, bool) {
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
			resolved, err := resolveOverwriteRoleIDs(stoatCh.Overwrites, guildID, mappings)
			if err != nil {
				return "", err
			}
			stoatCh.Overwrites = resolved

			// The engine now serializes all ops for a given entity, so a
			// stale pending row here should only ever be left over from a
			// crash, not a live in-flight create.
			return applyCreateOrEdit(mappings, engine.EntityChannel, ch.ID,
				func(stoatID string) error { return writer.EditChannel(ctx, stoatID, stoatCh) },
				func() (string, error) { return writer.CreateChannel(ctx, stoatServerID, stoatCh) },
			)
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
			return applyDelete(mappings, engine.EntityChannel, discordChannelID,
				func(stoatID string) error { return writer.DeleteChannel(ctx, stoatID) },
			)
		},
	}
}

// applyCreateOrEdit implements the "has a stoat id -> edit; else -> create"
// branch shared by every entity's Apply closure (see BuildChannelOp,
// BuildRoleOp). Deciding on Status instead of StoatID presence is a bug, not
// a stricter check: engine.process() always calls WritePending -- which
// flips Status to pending but preserves any existing StoatID -- immediately
// before calling Apply, so Apply's own mapping read here always observes
// Status=pending on every live update to an already-bound entity, active or
// not. A pending row only means "no entity yet" when StoatID is also empty
// (the first-time-create case, per store.WritePending's own doc comment);
// a pending row with a non-empty StoatID is an update whose remote
// confirm hasn't landed yet and must still edit, or it creates a duplicate
// and clobbers the mapping's stoat_id every time (confirmed via an isolated
// reproduction against the real engine+store, not assumed).
func applyCreateOrEdit(mappings MappingReader, entityType engine.EntityType, discordID string, edit func(stoatID string) error, create func() (string, error)) (string, error) {
	mapping, err := mappings.Get(string(entityType), discordID)
	if err != nil {
		return "", err
	}
	if mapping.HasStoatEntity() {
		if err := edit(mapping.StoatID); err != nil {
			return "", err
		}
		return mapping.StoatID, nil
	}
	return create()
}

// applyDelete implements the "no-op unless actively mapped, else delete"
// branch shared by every entity's delete Apply closure (see
// BuildChannelDeleteOp, BuildRoleDeleteOp).
func applyDelete(mappings MappingReader, entityType engine.EntityType, discordID string, del func(stoatID string) error) (string, error) {
	mapping, err := mappings.Get(string(entityType), discordID)
	if err != nil {
		return "", err
	}
	if !mapping.Found || mapping.Status != engine.StatusActive {
		return "", nil
	}
	if err := del(mapping.StoatID); err != nil {
		return "", err
	}
	return "", nil
}

// resolveOverwriteRoleIDs re-keys a StoatOverwrite map from Discord role ids
// to their mapped Stoat role ids. A role missing from the mapping table is
// skipped defensively -- DependsOn should already have gated the op until
// every referenced role is mapped, so this only guards against a race.
//
// Discord's @everyone role id always equals guildID -- Stoat has no role
// entity for "everyone" and its channel permission endpoint 404s
// (server.roles.get(&role_id) miss) on anything but the literal sentinel
// "default", so that case is resolved directly instead of through the
// role-mapping table.
func resolveOverwriteRoleIDs(overwrites map[string]canonical.StoatOverwrite, guildID string, mappings MappingReader) (map[string]canonical.StoatOverwrite, error) {
	resolved := make(map[string]canonical.StoatOverwrite, len(overwrites))
	for discordRoleID, ow := range overwrites {
		if discordRoleID == guildID {
			resolved[everyoneOverwriteRoleID] = ow
			continue
		}
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
