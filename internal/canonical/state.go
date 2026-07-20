package canonical

import (
	"encoding/json"
	"slices"
)

// overwriteJSON is Overwrite's deterministic wire shape: allow/deny sorted
// so byte-identical canonical states never register as a phantom diff
// (guardrail: canonical_state serialization must be deterministic).
type overwriteJSON struct {
	Allow []Permission `json:"allow"`
	Deny  []Permission `json:"deny"`
}

func (o Overwrite) canonicalize() overwriteJSON {
	allow := sortedPermissions(o.Allow)
	deny := sortedPermissions(o.Deny)
	return overwriteJSON{Allow: allow, Deny: deny}
}

func sortedPermissions(perms []Permission) []Permission {
	out := make([]Permission, len(perms))
	copy(out, perms)
	slices.Sort(out)
	return out
}

// channelJSON is Channel's deterministic canonical_state shape.
type channelJSON struct {
	ID         string                   `json:"id"`
	Name       string                   `json:"name"`
	Type       ChannelType              `json:"type"`
	CategoryID string                   `json:"category_id"`
	Position   int                      `json:"position"`
	Overwrites map[string]overwriteJSON `json:"overwrites"`
}

// CanonicalJSON serializes the channel with sorted role keys and sorted
// allow/deny permission lists, so equal states always produce byte-identical
// output regardless of map iteration or slice-build order.
func (c Channel) CanonicalJSON() ([]byte, error) {
	overwrites := make(map[string]overwriteJSON, len(c.Overwrites))
	for roleID, ow := range c.Overwrites {
		overwrites[roleID] = ow.canonicalize()
	}

	return json.Marshal(channelJSON{
		ID:         c.ID,
		Name:       c.Name,
		Type:       c.Type,
		CategoryID: c.CategoryID,
		Position:   c.Position,
		Overwrites: overwrites,
	})
}
