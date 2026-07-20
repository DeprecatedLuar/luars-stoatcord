package discord

import (
	"context"
	"log/slog"
	"reflect"

	"github.com/bwmarrin/discordgo"
	"github.com/luar/stoatcord/internal/canonical"
	"github.com/luar/stoatcord/internal/engine"
)

// RoleWriter is the subset of *internal/stoat.Client's role methods this
// package needs.
type RoleWriter interface {
	CreateRole(ctx context.Context, serverID string, role canonical.StoatRole) (string, error)
	EditRole(ctx context.Context, serverID, roleID string, role canonical.StoatRole) error
	DeleteRole(ctx context.Context, serverID, roleID string) error
}

// BuildRoleOp translates a Discord role create/update event into an
// engine.Op. Roles reference no other entity, so unlike BuildChannelOp there
// is no DependsOn to populate.
func BuildRoleOp(kind engine.OpKind, r *discordgo.Role, stoatServerID string, mappings MappingReader, writer RoleWriter, logger *slog.Logger) engine.Op {
	canonicalRole := RoleFromDiscord(r, logger)

	canonicalState, err := canonicalRole.CanonicalJSON()
	if err != nil {
		logger.Error("discord: failed to serialize role canonical state", "role_id", r.ID, "error", err)
	}

	return engine.Op{
		Kind:           kind,
		EntityType:     engine.EntityRole,
		DiscordID:      r.ID,
		CanonicalState: string(canonicalState),
		Diff: func(storedCanonicalState string) (bool, error) {
			stored, err := canonical.ParseRoleCanonicalJSON([]byte(storedCanonicalState))
			if err != nil {
				return false, err
			}
			return reflect.DeepEqual(stored.ToStoat(logger), canonicalRole.ToStoat(logger)), nil
		},
		Apply: func(ctx context.Context) (string, error) {
			stoatRole := canonicalRole.ToStoat(logger)

			mapping, err := mappings.Get(string(engine.EntityRole), r.ID)
			if err != nil {
				return "", err
			}
			// See discord.BuildChannelOp: a pending row has no usable
			// StoatID yet.
			if mapping.Found && mapping.Status == engine.StatusActive {
				if err := writer.EditRole(ctx, stoatServerID, mapping.StoatID, stoatRole); err != nil {
					return "", err
				}
				return mapping.StoatID, nil
			}
			return writer.CreateRole(ctx, stoatServerID, stoatRole)
		},
	}
}

// BuildRoleDeleteOp translates a Discord role delete event into an
// engine.Op. Apply is a no-op if the role was never mapped.
func BuildRoleDeleteOp(discordRoleID, stoatServerID string, mappings MappingReader, writer RoleWriter) engine.Op {
	return engine.Op{
		Kind:       engine.OpDelete,
		EntityType: engine.EntityRole,
		DiscordID:  discordRoleID,
		Apply: func(ctx context.Context) (string, error) {
			mapping, err := mappings.Get(string(engine.EntityRole), discordRoleID)
			if err != nil {
				return "", err
			}
			if !mapping.Found || mapping.Status != engine.StatusActive {
				return "", nil
			}
			if err := writer.DeleteRole(ctx, stoatServerID, mapping.StoatID); err != nil {
				return "", err
			}
			return "", nil
		},
	}
}
