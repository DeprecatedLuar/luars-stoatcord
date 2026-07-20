package discord

import (
	"testing"

	"github.com/bwmarrin/discordgo"
)

func TestServerFromDiscord_CopiesFields(t *testing.T) {
	g := &discordgo.Guild{
		ID:          "guild-1",
		Name:        "My Guild",
		Description: "a guild",
		Icon:        "icon-hash",
		Banner:      "banner-hash",
	}

	got := ServerFromDiscord(g)

	if got.ID != "guild-1" || got.Name != "My Guild" || got.Description != "a guild" || got.IconRef != "icon-hash" || got.BannerRef != "banner-hash" {
		t.Errorf("ServerFromDiscord() = %+v, unexpected fields", got)
	}
}

func TestEmojiFromDiscord_CopiesFields(t *testing.T) {
	e := &discordgo.Emoji{ID: "emoji-1", Name: "pog", Animated: true}

	got := EmojiFromDiscord(e)

	if got.ID != "emoji-1" || got.Name != "pog" || !got.Animated {
		t.Errorf("EmojiFromDiscord() = %+v, unexpected fields", got)
	}
	if got.NSFW {
		t.Errorf("NSFW = true, want false (Discord's Emoji has no per-emoji NSFW flag)")
	}
}
