package canonical

import "maps"

// ExpandCategoryPermissions applies a category's role-level permission
// overwrites onto a child channel (spec 6: Stoat categories have no
// permission concept, so category-level perms are expanded onto each child
// channel individually at sync time). A channel's own overwrite for a role
// takes precedence over the category's for that same role -- mirroring
// Discord's own category-then-channel layering. Pure: returns a new Channel,
// never mutates the input.
func ExpandCategoryPermissions(categoryOverwrites map[string]Overwrite, channel Channel) Channel {
	merged := make(map[string]Overwrite, len(categoryOverwrites)+len(channel.Overwrites))
	maps.Copy(merged, categoryOverwrites)
	maps.Copy(merged, channel.Overwrites)

	out := channel
	out.Overwrites = merged
	return out
}
