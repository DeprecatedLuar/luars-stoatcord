package discord

import (
	"github.com/bwmarrin/discordgo"
	"github.com/luar/stoatcord/internal/canonical"
)

// ServerFromDiscord translates a Discord guild's metadata to canonical
// (spec 6: name/desc/icon/banner, editable and synced). 1:1, no permission
// vocabulary involved.
func ServerFromDiscord(g *discordgo.Guild) canonical.Server {
	return canonical.Server{
		ID:          g.ID,
		Name:        g.Name,
		Description: g.Description,
		IconRef:     g.Icon,
		BannerRef:   g.Banner,
	}
}

// EmojiFromDiscord translates a Discord custom emoji to canonical (spec 6:
// auto-create on first use). Discord's Emoji has no per-emoji NSFW flag, so
// NSFW is always false here.
func EmojiFromDiscord(e *discordgo.Emoji) canonical.Emoji {
	return canonical.Emoji{
		ID:       e.ID,
		Name:     e.Name,
		Animated: e.Animated,
	}
}
