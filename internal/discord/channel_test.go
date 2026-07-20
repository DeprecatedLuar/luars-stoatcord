package discord

import (
	"bytes"
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"
	"github.com/luar/stoatcord/internal/canonical"
)

func TestChannelTypeFromDiscord_FlattensPerSpec6(t *testing.T) {
	cases := []struct {
		name string
		in   discordgo.ChannelType
		want canonical.ChannelType
	}{
		{"text", discordgo.ChannelTypeGuildText, canonical.ChannelTypeText},
		{"voice", discordgo.ChannelTypeGuildVoice, canonical.ChannelTypeVoice},
		{"announcement flattens to text", discordgo.ChannelTypeGuildNews, canonical.ChannelTypeText},
		{"stage flattens to voice", discordgo.ChannelTypeGuildStageVoice, canonical.ChannelTypeVoice},
		{"forum flattens to text", discordgo.ChannelTypeGuildForum, canonical.ChannelTypeText},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ChannelTypeFromDiscord(tc.in)
			if !ok {
				t.Fatalf("ChannelTypeFromDiscord(%v) not ok, want %v", tc.in, tc.want)
			}
			if got != tc.want {
				t.Errorf("ChannelTypeFromDiscord(%v) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestChannelTypeFromDiscord_CategoryIsNotAChannelType(t *testing.T) {
	_, ok := ChannelTypeFromDiscord(discordgo.ChannelTypeGuildCategory)
	if ok {
		t.Error("expected category channel type to be unsupported (categories are server-level, spec 6)")
	}
}

func TestChannelTypeFromDiscord_ThreadsAreDeferred(t *testing.T) {
	_, ok := ChannelTypeFromDiscord(discordgo.ChannelTypeGuildPublicThread)
	if ok {
		t.Error("expected thread channel type to be unsupported (deferred post-v1, spec 9)")
	}
}

func TestOverwriteFromDiscord_RoleTypeDecodesAllowDeny(t *testing.T) {
	var buf bytes.Buffer
	logger := newTestLogger(&buf)
	ow := &discordgo.PermissionOverwrite{
		ID:    "role-1",
		Type:  discordgo.PermissionOverwriteTypeRole,
		Allow: discordgo.PermissionViewChannel,
		Deny:  discordgo.PermissionSendMessages,
	}

	roleID, got, ok := OverwriteFromDiscord(ow, logger)

	if !ok {
		t.Fatal("expected role-type overwrite to translate")
	}
	if roleID != "role-1" {
		t.Errorf("roleID = %q, want role-1", roleID)
	}
	if len(got.Allow) != 1 || got.Allow[0] != canonical.PermViewChannel {
		t.Errorf("Allow = %v, want [VIEW_CHANNEL]", got.Allow)
	}
	if len(got.Deny) != 1 || got.Deny[0] != canonical.PermSendMessages {
		t.Errorf("Deny = %v, want [SEND_MESSAGES]", got.Deny)
	}
}

func TestOverwriteFromDiscord_MemberTypeDropsAndLogs(t *testing.T) {
	var buf bytes.Buffer
	logger := newTestLogger(&buf)
	ow := &discordgo.PermissionOverwrite{
		ID:    "user-1",
		Type:  discordgo.PermissionOverwriteTypeMember,
		Allow: discordgo.PermissionViewChannel,
	}

	_, _, ok := OverwriteFromDiscord(ow, logger)

	if ok {
		t.Error("expected member-level overwrite to be dropped (spec 7: no Stoat target entity)")
	}
	if !strings.Contains(buf.String(), "user-1") {
		t.Errorf("expected drop log to reference the member id, got: %s", buf.String())
	}
}

func TestChannelFromDiscord_AssemblesChannelWithRoleOverwrites(t *testing.T) {
	var buf bytes.Buffer
	logger := newTestLogger(&buf)
	ch := &discordgo.Channel{
		ID:       "chan-1",
		Name:     "general",
		Type:     discordgo.ChannelTypeGuildText,
		ParentID: "cat-1",
		Position: 3,
		PermissionOverwrites: []*discordgo.PermissionOverwrite{
			{ID: "role-1", Type: discordgo.PermissionOverwriteTypeRole, Allow: discordgo.PermissionViewChannel},
			{ID: "user-1", Type: discordgo.PermissionOverwriteTypeMember, Allow: discordgo.PermissionViewChannel},
		},
	}

	got, ok := ChannelFromDiscord(ch, logger)

	if !ok {
		t.Fatal("expected text channel to translate")
	}
	if got.ID != "chan-1" || got.Name != "general" || got.Type != canonical.ChannelTypeText || got.CategoryID != "cat-1" || got.Position != 3 {
		t.Errorf("ChannelFromDiscord() = %+v, unexpected fields", got)
	}
	if _, ok := got.Overwrites["role-1"]; !ok {
		t.Errorf("expected role-1 overwrite to be present, got %+v", got.Overwrites)
	}
	if _, ok := got.Overwrites["user-1"]; ok {
		t.Errorf("expected member-level overwrite to be dropped, got %+v", got.Overwrites)
	}
}

func TestChannelFromDiscord_UnsupportedTypeReturnsNotOk(t *testing.T) {
	var buf bytes.Buffer
	logger := newTestLogger(&buf)
	ch := &discordgo.Channel{ID: "cat-1", Name: "Category", Type: discordgo.ChannelTypeGuildCategory}

	_, ok := ChannelFromDiscord(ch, logger)

	if ok {
		t.Error("expected category channel to not translate via ChannelFromDiscord")
	}
}
