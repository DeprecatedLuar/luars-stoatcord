package stoat

import (
	"context"
	"fmt"

	"github.com/luar/stoatcord/internal/canonical"
	"within.website/x/web/revolt"
)

// SendMessage posts a message via masquerade, returning its Stoat id. If
// msg.ReplyToStoatID is set, the message is sent as a reply -- mention is
// deliberately always false: every mirrored message posts under the same
// bot account (spec 7's masquerade-only model), so a real mention would just
// ping the bot itself, not the Discord author whose reply this represents.
func (c *Client) SendMessage(ctx context.Context, channelID string, msg canonical.StoatMessage) (string, error) {
	var replies []revolt.Replies
	if msg.ReplyToStoatID != "" {
		replies = []revolt.Replies{{Id: msg.ReplyToStoatID, Mention: false}}
	}

	sent, err := c.inner.ChannelSendMessage(ctx, channelID, &revolt.SendMessage{
		Content:     msg.Content,
		Attachments: msg.Attachments,
		Replies:     replies,
		Masquerade: &revolt.Masquerade{
			Name:      msg.Masquerade.Name,
			AvatarURL: msg.Masquerade.Avatar,
			Color:     msg.Masquerade.Colour,
		},
	})
	if err != nil {
		return "", fmt.Errorf("stoat: send message to channel %s: %w", channelID, err)
	}
	return sent.ID, nil
}

// EditMessage updates a message's content only -- masquerade survives a
// Stoat message edit untouched server-side (confirmed Phase 0), so it is not
// resent here.
func (c *Client) EditMessage(ctx context.Context, channelID, messageID, content string) error {
	if err := c.inner.MessageEdit(ctx, channelID, messageID, content); err != nil {
		return fmt.Errorf("stoat: edit message %s in channel %s: %w", messageID, channelID, err)
	}
	return nil
}

// DeleteMessage deletes a message by its Stoat channel and message id.
func (c *Client) DeleteMessage(ctx context.Context, channelID, messageID string) error {
	if err := c.inner.MessageDelete(ctx, channelID, messageID); err != nil {
		return fmt.Errorf("stoat: delete message %s in channel %s: %w", messageID, channelID, err)
	}
	return nil
}
