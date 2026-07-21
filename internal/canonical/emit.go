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

// stoatChannelTypes maps the canonical (already-flattened, spec 6) channel
// type to Stoat's wire-shaped type string.
var stoatChannelTypes = map[ChannelType]string{
	ChannelTypeText:  "Text",
	ChannelTypeVoice: "Voice",
}

// StoatChannel is Stoat's wire-shaped channel. Overwrites is keyed by
// canonical role id, same as Channel.Overwrites.
type StoatChannel struct {
	Name       string
	Type       string
	Overwrites map[string]StoatOverwrite
}

// ToStoat translates a canonical channel to Stoat's wire shape, logging any
// dropped permissions in its overwrites (spec 4). Category membership and
// position are not translated here -- they live on Category.ChannelIDs
// (spec 6), never on the channel itself.
func (c Channel) ToStoat(logger *slog.Logger) StoatChannel {
	overwrites := make(map[string]StoatOverwrite, len(c.Overwrites))
	for roleID, ow := range c.Overwrites {
		overwrites[roleID] = ow.ToStoat(logger)
	}
	return StoatChannel{
		Name:       c.Name,
		Type:       stoatChannelTypes[c.Type],
		Overwrites: overwrites,
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
	Position   int      `json:"position"`
}

// CanonicalJSON serializes the category. Unlike Channel's permission lists,
// ChannelIDs is never sorted -- its order is meaningful sidebar position.
func (c Category) CanonicalJSON() ([]byte, error) {
	return json.Marshal(categoryJSON{ID: c.ID, Name: c.Name, ChannelIDs: c.ChannelIDs, Position: c.Position})
}

// ParseCategoryCanonicalJSON reverses Category.CanonicalJSON (same purpose
// as ParseChannelCanonicalJSON: rebuild a stored canonical_state row for the
// engine's Diff step).
func ParseCategoryCanonicalJSON(data []byte) (Category, error) {
	var cj categoryJSON
	if err := json.Unmarshal(data, &cj); err != nil {
		return Category{}, err
	}
	return Category{ID: cj.ID, Name: cj.Name, ChannelIDs: cj.ChannelIDs, Position: cj.Position}, nil
}
