package discord

import (
	"fmt"
	"log/slog"

	"github.com/bwmarrin/discordgo"
	"github.com/luar/stoatcord/internal/canonical"
)

// RoleFromDiscord translates a Discord role to canonical (spec 6: 1:1 on
// name, colour, hoist, rank, permissions). Discord roles carry no deny bits
// -- only channel-level overwrites do -- so Permissions.Deny is always
// empty here.
func RoleFromDiscord(r *discordgo.Role, logger *slog.Logger) canonical.Role {
	return canonical.Role{
		ID:     r.ID,
		Name:   r.Name,
		Colour: fmt.Sprintf("#%06X", r.Color),
		Hoist:  r.Hoist,
		Rank:   r.Position,
		Permissions: canonical.Overwrite{
			Allow: PermissionsFromBits(r.Permissions, logger),
		},
		Privileged: r.Permissions&discordgo.PermissionAdministrator != 0,
	}
}
