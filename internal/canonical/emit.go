package canonical

import (
	"encoding/json"
	"log/slog"
)

// StoatRole is Stoat's wire-shaped role.
type StoatRole struct {
	Name        string
	Colour      string
	Hoist       bool
	Rank        int
	Permissions StoatOverwrite
}

// ToStoat translates a canonical role to Stoat's wire shape, logging any
// dropped permissions (spec 4).
func (r Role) ToStoat(logger *slog.Logger) StoatRole {
	return StoatRole{
		Name:        r.Name,
		Colour:      r.Colour,
		Hoist:       r.Hoist,
		Rank:        r.Rank,
		Permissions: r.Permissions.ToStoat(logger),
	}
}

// StoatEmoji is Stoat's wire-shaped custom emoji.
type StoatEmoji struct {
	Name     string
	Animated bool
	NSFW     bool
}

// ToStoat translates a canonical emoji to Stoat's wire shape. 1:1, no
// permission vocabulary involved.
func (e Emoji) ToStoat() StoatEmoji {
	return StoatEmoji{Name: e.Name, Animated: e.Animated, NSFW: e.NSFW}
}

// StoatServer is Stoat's wire-shaped server metadata.
type StoatServer struct {
	Name        string
	Description string
	IconRef     string
	BannerRef   string
}

// ToStoat translates canonical server metadata to Stoat's wire shape.
func (s Server) ToStoat() StoatServer {
	return StoatServer{
		Name:        s.Name,
		Description: s.Description,
		IconRef:     s.IconRef,
		BannerRef:   s.BannerRef,
	}
}

// StoatMasquerade is Stoat's per-message masquerade override (spec 7): the
// only identity a mirrored message carries, since the bot owns every post.
type StoatMasquerade struct {
	Name   string
	Avatar string
	Colour string
}

// StoatMessage is Stoat's wire-shaped outgoing message.
type StoatMessage struct {
	Content     string
	Masquerade  StoatMasquerade
	Attachments []string
}

// ToStoat translates a canonical message to Stoat's wire shape, building the
// masquerade override from the stored author display fields.
func (m Message) ToStoat() StoatMessage {
	return StoatMessage{
		Content: m.Content,
		Masquerade: StoatMasquerade{
			Name:   m.AuthorName,
			Avatar: m.AuthorAvatarRef,
			Colour: m.AuthorColour,
		},
		Attachments: m.AttachmentRefs,
	}
}

// categoryJSON is Category's deterministic canonical_state shape.
// ChannelIDs order is preserved deliberately -- it is the display order
// (spec 6), not an unordered set.
type categoryJSON struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	ChannelIDs []string `json:"channel_ids"`
}

// CanonicalJSON serializes the category. Unlike Channel's permission lists,
// ChannelIDs is never sorted -- its order is meaningful sidebar position.
func (c Category) CanonicalJSON() ([]byte, error) {
	return json.Marshal(categoryJSON{ID: c.ID, Name: c.Name, ChannelIDs: c.ChannelIDs})
}
