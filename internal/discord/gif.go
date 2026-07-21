package discord

import (
	"log/slog"
	"regexp"

	"github.com/bwmarrin/discordgo"
	"github.com/luar/stoatcord/internal/canonical"
	"github.com/luar/stoatcord/internal/engine"
)

// gifHostPattern matches known GIF-picker/GIF-hosting service URLs appearing
// in Discord message content. Discord's built-in GIF picker (Tenor/Klipy/
// etc.) doesn't produce a real discordgo.Message.Attachments[] entry -- it
// sends the GIF as a plain URL in Content, which Discord's own client
// unfurls into a preview embed asynchronously (implementation-plan.md Phase
// 5.3b). Detection here is deliberately synchronous and heuristic: it acts
// on Content at MESSAGE_CREATE time rather than waiting for Discord's own
// embed, since we do our own fetch and don't need Discord's embed metadata.
// An unlisted host is simply not detected -- see handlers.go's mismatch log
// for the rare case where Discord's own embed later confirms a miss.
var gifHostPattern = regexp.MustCompile(`(?i)https?://(?:www\.)?(?:tenor\.com/view|giphy\.com/gifs|klipy\.com|gfycat\.com|redgifs\.com/watch|gifbox\.me/view|yiffbox\.me/view)\S*`)

// isGifLink reports whether content is or contains a recognized GIF-host
// URL, returning the matched URL substring (suitable for UploadFromURL).
func isGifLink(content string) (url string, ok bool) {
	match := gifHostPattern.FindString(content)
	if match == "" {
		return "", false
	}
	return match, true
}

// logGifDetectionMismatch is the correction path for isGifLink's heuristic
// (see BuildMessageOp): if Discord's own delayed embed later confirms a
// message held playable media (Type gifv/image/video) that isGifLink didn't
// catch at create time (unlisted host), this logs the mismatch for
// visibility. Deliberately diagnostic-only -- no delete/edit/re-upload is
// attempted (Stoat's edit endpoint has no attachments field at all, so
// there's nothing safe to do here beyond logging; see implementation-plan.md
// Phase 5.3b). Called independently of the op pipeline, from the
// MESSAGE_UPDATE handler, since this is not itself an op.
func logGifDetectionMismatch(m *discordgo.Message, mappings MappingReader, logger *slog.Logger) {
	var mediaEmbed *discordgo.MessageEmbed
	for _, e := range m.Embeds {
		if e.Type == discordgo.EmbedTypeGifv || e.Type == discordgo.EmbedTypeImage || e.Type == discordgo.EmbedTypeVideo {
			mediaEmbed = e
			break
		}
	}
	if mediaEmbed == nil {
		return
	}

	mapping, err := mappings.Get(string(engine.EntityMessage), m.ID)
	if err != nil || !mapping.Found {
		return
	}

	stored, err := canonical.ParseMessageCanonicalJSON([]byte(mapping.CanonicalState))
	if err != nil {
		return
	}

	if _, ok := isGifLink(stored.Content); ok {
		return
	}

	logger.Warn("discord: gif-link heuristic missed a Discord-confirmed media embed",
		"message_id", m.ID, "embed_type", string(mediaEmbed.Type), "embed_url", mediaEmbed.URL)
}
