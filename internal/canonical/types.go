package canonical

// Entity types (spec 3): the neutral shape every platform translator reads
// from and writes to. No discordgo or revolt types appear here.

// Server is the mirrored guild/server itself.
type Server struct {
	ID          string
	Name        string
	Description string
	IconRef     string
	BannerRef   string
}

// Category is a server-level ordered list of channel ids (spec 6): moving a
// channel between categories mutates this list, never a Channel field.
type Category struct {
	ID         string
	Name       string
	ChannelIDs []string
	// Position is this category's own index among all of the server's
	// categories (sidebar order) -- distinct from Channel.Position, which
	// orders channels within a single category.
	Position int
}

// ChannelType is the canonical channel kind after flattening (spec 6):
// announcement/stage/forum collapse onto these two.
type ChannelType string

const (
	ChannelTypeText  ChannelType = "text"
	ChannelTypeVoice ChannelType = "voice"
)

// Channel is a single mirrored channel. Overwrites is keyed by canonical
// role id; category membership lives on Category.ChannelIDs, not here.
type Channel struct {
	ID         string
	Name       string
	Type       ChannelType
	CategoryID string
	Position   int
	Overwrites map[string]Overwrite
}

// Role is a mirrored server role. Permissions is tri-state per spec 3 /
// Phase 0 finding (Stoat roles carry {a: allow, d: deny} too).
type Role struct {
	ID          string
	Name        string
	Colour      string
	Hoist       bool
	Rank        int
	Permissions Overwrite
	// Privileged marks a role whose holders should bypass per-channel
	// visibility restrictions (Discord's ADMINISTRATOR permission, which has
	// no Stoat bit equivalent and is otherwise dropped entirely at ingest).
	Privileged bool
}

// Emoji is a custom emoji, auto-created on first use (spec 6).
type Emoji struct {
	ID       string
	Name     string
	Animated bool
	NSFW     bool
}

// Message is a single mirrored message, posted via masquerade (spec 7) --
// there is no canonical "author" identity beyond the display fields below.
type Message struct {
	ID              string
	ChannelID       string
	AuthorName      string
	AuthorAvatarRef string
	AuthorColour    string
	Content         string
	AttachmentRefs  []string
	// ReplyToID is the Discord id of the message this one replies to, empty
	// if it isn't a reply. Left unresolved here deliberately -- translating
	// it to a Stoat message id needs a mapping lookup, which is a
	// discord-package/apply-time concern, not something internal/canonical
	// can do (it knows nothing of either platform's live state).
	ReplyToID string
}
