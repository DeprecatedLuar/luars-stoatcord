package discord

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/luar/stoatcord/internal/canonical"
	"github.com/luar/stoatcord/internal/engine"
)

// MessageWriter is the subset of *internal/stoat.Client's message methods
// this package needs.
type MessageWriter interface {
	SendMessage(ctx context.Context, stoatChannelID string, msg canonical.StoatMessage) (string, error)
	EditMessage(ctx context.Context, stoatChannelID, stoatMessageID, content string) error
	DeleteMessage(ctx context.Context, stoatChannelID, stoatMessageID string) error
	UploadFromURL(ctx context.Context, tag, url string) (string, error)
}

// attachmentAutumnTag is Autumn's upload tag for message attachments (spec
// "Message pipeline details": Discord CDN links expire, so an attachment is
// downloaded and re-uploaded here before it can be attached to a Stoat
// message).
const attachmentAutumnTag = "attachments"

// maxStoatAttachmentsPerMessage is Stoat's own per-message attachment cap
// (confirmed live, Phase 0) -- Discord allows more, so any beyond this are
// dropped with a log rather than silently truncated.
const maxStoatAttachmentsPerMessage = 5

// BuildMessageOp translates a Discord message create/update event into an
// engine.Op. One unified builder for create+update, deliberately mirroring
// BuildChannelOp/BuildRoleOp: Kind can go stale once an op is deferred
// behind an unmet dependency, so Apply resolves create-vs-edit fresh at
// apply-time off mapping.HasStoatEntity(), never off Kind.
//
// ok is false if m.Content == "" and (for an edit, always; for a create,
// there are also no attachments). Content-empty-and-no-attachments covers a
// message with nothing minable yet (embed-only), and Discord's own partial
// MESSAGE_UPDATE payloads, which omit content entirely (decoded by
// discordgo as "") when only non-content fields changed (e.g. link-embed
// auto-populate). Forwarding that to EditMessage would blank out the
// mirrored message's text, so an edit is skipped on empty content
// regardless of attachments -- Discord does not allow editing a message's
// attachments at all via its own UI, so attachments are a create-only
// concern and this cannot drop a legitimate edit.
func BuildMessageOp(kind engine.OpKind, s *discordgo.Session, guildID string, m *discordgo.Message, mappings MappingReader, writer MessageWriter, logger *slog.Logger) (engine.Op, bool) {
	if m.Content == "" && (kind != engine.OpCreate || len(m.Attachments) == 0) {
		logger.Info("discord: skipping message op with empty content", "message_id", m.ID)
		return engine.Op{}, false
	}

	authorName, authorAvatar, authorColour := authorDisplayFromDiscord(s, guildID, m, logger)

	var replyToID string
	// MessageReferenceTypeForward (forwarded messages) is a distinct Discord
	// feature from a reply and carries no equivalent Stoat concept yet --
	// only a Default-type reference is a genuine reply.
	if m.MessageReference != nil && m.MessageReference.Type == discordgo.MessageReferenceTypeDefault {
		replyToID = m.MessageReference.MessageID
	}

	// AttachmentRefs holds raw Discord CDN URLs at this point, not Stoat
	// attachment ids -- resolving those needs an Autumn upload (network
	// I/O), which is deferred to Apply below, mirroring how ReplyToID is
	// left as a raw Discord id here and resolved to ReplyToStoatID only at
	// apply-time. Anything beyond Stoat's own per-message cap is dropped
	// with a log rather than silently truncated.
	attachmentRefs := make([]string, 0, len(m.Attachments))
	for i, att := range m.Attachments {
		if i >= maxStoatAttachmentsPerMessage {
			logger.Warn("discord: dropping attachment beyond Stoat's per-message cap",
				"message_id", m.ID, "attachment_id", att.ID, "cap", maxStoatAttachmentsPerMessage)
			continue
		}
		attachmentRefs = append(attachmentRefs, att.URL)
	}

	// A Discord GIF-picker link (Tenor/Klipy/etc.) is plain content text,
	// not a real attachment (see gif.go) -- detected and stripped here, at
	// create time, so it rides the same AttachmentRefs -> UploadFromURL path
	// as a real attachment instead of mirroring through as a raw link.
	// Create-only, like every other AttachmentRefs use above: Stoat's edit
	// endpoint has no attachments field at all, so stripping the link on an
	// edit would delete it with nothing to replace it -- an edit leaves the
	// link as plain text instead, same as before this feature existed.
	content := m.Content
	if kind == engine.OpCreate {
		if url, ok := isGifLink(content); ok && len(attachmentRefs) < maxStoatAttachmentsPerMessage {
			attachmentRefs = append(attachmentRefs, url)
			content = strings.TrimSpace(strings.Replace(content, url, "", 1))
		}
	}

	canonicalMsg := canonical.Message{
		ID:              m.ID,
		ChannelID:       m.ChannelID,
		Content:         content,
		AuthorName:      authorName,
		AuthorAvatarRef: authorAvatar,
		AuthorColour:    authorColour,
		AttachmentRefs:  attachmentRefs,
		ReplyToID:       replyToID,
	}

	canonicalState, err := canonicalMsg.CanonicalJSON()
	if err != nil {
		logger.Error("discord: failed to serialize message canonical state", "message_id", m.ID, "error", err)
		return engine.Op{}, false
	}

	op := engine.Op{
		Kind:       kind,
		EntityType: engine.EntityMessage,
		DiscordID:  m.ID,
		// Required, not optional: workerKey() only routes to the shared
		// per-channel serialization worker when ChannelID != "". Leaving it
		// zero-value would still serialize a given message's own
		// create/edit/delete together, but would let different messages in
		// the same channel run on independent goroutines, breaking
		// Discord's send-order = display-order guarantee (spec 5).
		ChannelID:      m.ChannelID,
		CanonicalState: string(canonicalState),
		DependsOn: []engine.DependencyKey{
			{EntityType: engine.EntityChannel, DiscordID: m.ChannelID},
		},
		// Deliberate deviation from BuildChannelOp's Diff pattern: compares
		// Content only, not a full ToStoat() equality. An edit's Apply only
		// ever pushes Content to Stoat (masquerade is not resent on edit),
		// so comparing author fields would produce false "not equal"
		// results whenever an edit event's partial payload has
		// degraded/empty author fields versus the real ones stored at
		// create time -- defeating the point of skipping a redundant Stoat
		// call on a pure embed-unfurl update.
		Diff: func(storedCanonicalState string) (bool, error) {
			stored, err := canonical.ParseMessageCanonicalJSON([]byte(storedCanonicalState))
			if err != nil {
				return false, err
			}
			return stored.Content == canonicalMsg.Content, nil
		},
		Apply: func(ctx context.Context) (string, error) {
			channelMapping, err := mappings.Get(string(engine.EntityChannel), m.ChannelID)
			if err != nil {
				return "", err
			}
			stoatChannelID := channelMapping.StoatID

			return applyCreateOrEdit(mappings, engine.EntityMessage, m.ID,
				func(stoatID string) error {
					return writer.EditMessage(ctx, stoatChannelID, stoatID, canonicalMsg.Content)
				},
				func() (string, error) {
					stoatMsg := canonicalMsg.ToStoat()
					// Resolve each attachment's Autumn id here, not in
					// ToStoat -- canonical has no network access. AttachmentRefs
					// carries raw Discord CDN URLs up to this point (set above);
					// overwrite with the real uploaded ids before send.
					if len(canonicalMsg.AttachmentRefs) > 0 {
						uploadedIDs := make([]string, 0, len(canonicalMsg.AttachmentRefs))
						for _, url := range canonicalMsg.AttachmentRefs {
							id, err := writer.UploadFromURL(ctx, attachmentAutumnTag, url)
							if err != nil {
								return "", fmt.Errorf("discord: upload attachment for message %s: %w", m.ID, err)
							}
							uploadedIDs = append(uploadedIDs, id)
						}
						stoatMsg.Attachments = uploadedIDs
					}
					// Resolve the reply target's Stoat id here, not in
					// ToStoat -- canonical has no mapping access.
					if canonicalMsg.ReplyToID != "" {
						replyMapping, err := mappings.Get(string(engine.EntityMessage), canonicalMsg.ReplyToID)
						if err != nil {
							return "", err
						}
						if replyMapping.Found && replyMapping.StoatID != "" {
							stoatMsg.ReplyToStoatID = replyMapping.StoatID
						} else {
							// Parent was never mirrored (empty content, sent
							// before the bridge existed, or already pruned by
							// the 30-day retention window) -- no Stoat
							// message exists to link against. Fall back to
							// quoting its content inline rather than losing
							// the reply context entirely.
							stoatMsg.Content = quoteUnmappedReply(s, m.ChannelID, canonicalMsg.ReplyToID, stoatMsg.Content, logger)
						}
					}
					return writer.SendMessage(ctx, stoatChannelID, stoatMsg)
				},
			)
		},
	}
	return op, true
}

// BuildMessageDeleteOp translates a Discord message delete event into an
// engine.Op. discordgo.MessageDelete's payload only ever carries
// id/channel_id/guild_id -- this signature already matches that, no
// Author/Content fields expected on a delete event.
func BuildMessageDeleteOp(discordMsgID, discordChannelID string, mappings MappingReader, writer MessageWriter) engine.Op {
	return engine.Op{
		Kind:       engine.OpDelete,
		EntityType: engine.EntityMessage,
		DiscordID:  discordMsgID,
		ChannelID:  discordChannelID,
		Apply: func(ctx context.Context) (string, error) {
			channelMapping, err := mappings.Get(string(engine.EntityChannel), discordChannelID)
			if err != nil {
				return "", err
			}
			return applyDelete(mappings, engine.EntityMessage, discordMsgID,
				func(stoatID string) error { return writer.DeleteMessage(ctx, channelMapping.StoatID, stoatID) },
			)
		},
	}
}

// maxQuotedReplyLen caps how much of an unmapped reply target's content gets
// quoted inline, so a long parent message can't dwarf the reply itself.
const maxQuotedReplyLen = 150

// quoteUnmappedReply is the fallback for replying to a Discord message that
// has no Stoat mapping (see BuildMessageOp) -- there's no Stoat message id to
// attach a real reply to, so it fetches the parent from Discord and prepends
// a quoted snippet to the reply's own content instead. Any failure to fetch
// (parent deleted, API error) or an empty parent just logs and sends the
// reply as a plain message, same as before this fallback existed.
func quoteUnmappedReply(s *discordgo.Session, channelID, replyToID, content string, logger *slog.Logger) string {
	parent, err := s.ChannelMessage(channelID, replyToID)
	if err != nil || parent.Content == "" {
		logger.Info("discord: could not fetch unmapped reply target, sending as plain message", "reply_to_id", replyToID, "error", err)
		return content
	}

	snippet := []rune(parent.Content)
	if len(snippet) > maxQuotedReplyLen {
		snippet = append(snippet[:maxQuotedReplyLen], '…')
	}

	return fmt.Sprintf("> **%s**: %s\n\n%s", parent.Author.Username, string(snippet), content)
}

// authorDisplayFromDiscord resolves a message's masquerade display fields:
// name = member nickname if set, else username; avatar = the sender's
// Discord CDN avatar URL, passed straight through (no Autumn re-upload
// needed for masquerade avatars, unlike message attachments); colour = the
// highest-position non-zero-colour role among the member's roles, mirroring
// Discord's own top-role-colour display rule, empty if none.
//
// m.Member is expected to ride along with MESSAGE_CREATE without needing the
// privileged GUILD_MEMBERS intent -- if it turns out nil in practice, this
// degrades to username + empty colour rather than erroring.
func authorDisplayFromDiscord(s *discordgo.Session, guildID string, m *discordgo.Message, logger *slog.Logger) (name, avatarURL, colourHex string) {
	name = m.Author.Username
	avatarURL = m.Author.AvatarURL("")

	if m.Member == nil {
		logger.Info("discord: message has no Member payload, falling back to username + no colour", "message_id", m.ID)
		return name, avatarURL, ""
	}

	if m.Member.Nick != "" {
		name = m.Member.Nick
	}

	topPosition := -1
	for _, roleID := range m.Member.Roles {
		role, err := s.State.Role(guildID, roleID)
		if err != nil || role.Color == 0 {
			continue
		}
		if role.Position > topPosition {
			topPosition = role.Position
			colourHex = fmt.Sprintf("#%06X", role.Color)
		}
	}

	return name, avatarURL, colourHex
}
