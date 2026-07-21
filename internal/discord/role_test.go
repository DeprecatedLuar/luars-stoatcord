package discord

import (
	"bytes"
	"testing"

	"github.com/bwmarrin/discordgo"
	"github.com/luar/stoatcord/internal/canonical"
)

func TestRoleFromDiscord_AssemblesRoleFields(t *testing.T) {
	var buf bytes.Buffer
	logger := newTestLogger(&buf)
	r := &discordgo.Role{
		ID:          "role-1",
		Name:        "Moderator",
		Color:       0xFF00AA,
		Hoist:       true,
		Position:    5,
		Permissions: discordgo.PermissionViewChannel | discordgo.PermissionKickMembers,
	}

	got := RoleFromDiscord(r, logger)

	if got.ID != "role-1" || got.Name != "Moderator" || !got.Hoist || got.Rank != 5 {
		t.Errorf("RoleFromDiscord() = %+v, unexpected fields", got)
	}
	if got.Colour != "#FF00AA" {
		t.Errorf("Colour = %q, want #FF00AA", got.Colour)
	}
	want := map[canonical.Permission]bool{canonical.PermViewChannel: true, canonical.PermKickMembers: true}
	if len(got.Permissions.Allow) != 2 {
		t.Fatalf("Permissions.Allow = %v, want 2 entries", got.Permissions.Allow)
	}
	for _, p := range got.Permissions.Allow {
		if !want[p] {
			t.Errorf("unexpected permission %v", p)
		}
	}
	if len(got.Permissions.Deny) != 0 {
		t.Errorf("Permissions.Deny = %v, want empty (Discord roles carry no deny)", got.Permissions.Deny)
	}
}

func TestRoleFromDiscord_DropsUnmappedPermissionAndLogs(t *testing.T) {
	var buf bytes.Buffer
	logger := newTestLogger(&buf)
	r := &discordgo.Role{ID: "role-1", Name: "X", Permissions: discordgo.PermissionAdministrator}

	got := RoleFromDiscord(r, logger)

	if len(got.Permissions.Allow) != 0 {
		t.Errorf("Permissions.Allow = %v, want empty", got.Permissions.Allow)
	}
	if buf.Len() == 0 {
		t.Error("expected a drop log for Administrator permission")
	}
}

func TestRoleFromDiscord_AdministratorSetsPrivileged(t *testing.T) {
	var buf bytes.Buffer
	logger := newTestLogger(&buf)
	r := &discordgo.Role{ID: "role-1", Name: "Admin", Permissions: discordgo.PermissionAdministrator}

	got := RoleFromDiscord(r, logger)

	if !got.Privileged {
		t.Error("Privileged = false, want true for a role with ADMINISTRATOR")
	}
}

func TestRoleFromDiscord_NoAdministratorLeavesPrivilegedFalse(t *testing.T) {
	var buf bytes.Buffer
	logger := newTestLogger(&buf)
	r := &discordgo.Role{ID: "role-1", Name: "Member", Permissions: discordgo.PermissionViewChannel}

	got := RoleFromDiscord(r, logger)

	if got.Privileged {
		t.Error("Privileged = true, want false without ADMINISTRATOR")
	}
}
