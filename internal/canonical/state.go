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

// ParseChannelCanonicalJSON reverses Channel.CanonicalJSON, so a stored
// canonical_state row can be rebuilt into a Channel for the engine's Diff
// step (guardrail: diff translates both the desired state and the stored
// snapshot to Stoat-shape at compare time, so the stored snapshot must be
// parseable back to canonical first).
func ParseChannelCanonicalJSON(data []byte) (Channel, error) {
	var cj channelJSON
	if err := json.Unmarshal(data, &cj); err != nil {
		return Channel{}, err
	}

	overwrites := make(map[string]Overwrite, len(cj.Overwrites))
	for roleID, ow := range cj.Overwrites {
		overwrites[roleID] = Overwrite{Allow: ow.Allow, Deny: ow.Deny}
	}

	return Channel{
		ID:         cj.ID,
		Name:       cj.Name,
		Type:       cj.Type,
		CategoryID: cj.CategoryID,
		Position:   cj.Position,
		Overwrites: overwrites,
	}, nil
}

// roleJSON is Role's deterministic canonical_state shape.
type roleJSON struct {
	ID          string        `json:"id"`
	Name        string        `json:"name"`
	Colour      string        `json:"colour"`
	Hoist       bool          `json:"hoist"`
	Rank        int           `json:"rank"`
	Permissions overwriteJSON `json:"permissions"`
}

// CanonicalJSON serializes the role with sorted allow/deny permission lists,
// so equal states always produce byte-identical output regardless of slice
// order.
func (r Role) CanonicalJSON() ([]byte, error) {
	return json.Marshal(roleJSON{
		ID:          r.ID,
		Name:        r.Name,
		Colour:      r.Colour,
		Hoist:       r.Hoist,
		Rank:        r.Rank,
		Permissions: r.Permissions.canonicalize(),
	})
}

// ParseRoleCanonicalJSON reverses Role.CanonicalJSON (same purpose as
// ParseChannelCanonicalJSON: rebuild a stored canonical_state row for the
// engine's Diff step).
func ParseRoleCanonicalJSON(data []byte) (Role, error) {
	var rj roleJSON
	if err := json.Unmarshal(data, &rj); err != nil {
		return Role{}, err
	}

	return Role{
		ID:     rj.ID,
		Name:   rj.Name,
		Colour: rj.Colour,
		Hoist:  rj.Hoist,
		Rank:   rj.Rank,
		Permissions: Overwrite{
			Allow: rj.Permissions.Allow,
			Deny:  rj.Permissions.Deny,
		},
	}, nil
}
