package canonical

import "testing"

func TestExpandCategoryPermissions_AppliesCategoryOverwriteWhenChannelHasNone(t *testing.T) {
	categoryOverwrites := map[string]Overwrite{
		"role-a": {Allow: []Permission{PermViewChannel}},
	}
	channel := Channel{ID: "c1", Name: "general"}

	got := ExpandCategoryPermissions(categoryOverwrites, channel)

	ow, ok := got.Overwrites["role-a"]
	if !ok {
		t.Fatalf("expected category overwrite for role-a to be applied, got %+v", got.Overwrites)
	}
	if len(ow.Allow) != 1 || ow.Allow[0] != PermViewChannel {
		t.Errorf("role-a overwrite = %+v, want Allow=[VIEW_CHANNEL]", ow)
	}
}

func TestExpandCategoryPermissions_ChannelOwnOverwriteTakesPrecedence(t *testing.T) {
	categoryOverwrites := map[string]Overwrite{
		"role-a": {Deny: []Permission{PermViewChannel}},
	}
	channel := Channel{
		ID:   "c1",
		Name: "general",
		Overwrites: map[string]Overwrite{
			"role-a": {Allow: []Permission{PermViewChannel}},
		},
	}

	got := ExpandCategoryPermissions(categoryOverwrites, channel)

	ow := got.Overwrites["role-a"]
	if len(ow.Deny) != 0 || len(ow.Allow) != 1 || ow.Allow[0] != PermViewChannel {
		t.Errorf("channel's own overwrite should win, got %+v", ow)
	}
}

func TestExpandCategoryPermissions_DoesNotMutateInputChannel(t *testing.T) {
	categoryOverwrites := map[string]Overwrite{
		"role-a": {Allow: []Permission{PermViewChannel}},
	}
	channel := Channel{ID: "c1", Name: "general"}

	ExpandCategoryPermissions(categoryOverwrites, channel)

	if channel.Overwrites != nil {
		t.Errorf("expected original channel.Overwrites to remain nil, got %+v", channel.Overwrites)
	}
}

func TestExpandCategoryPermissions_EmptyCategoryOverwritesLeavesChannelUnchanged(t *testing.T) {
	channel := Channel{
		ID:   "c1",
		Name: "general",
		Overwrites: map[string]Overwrite{
			"role-a": {Allow: []Permission{PermSendMessages}},
		},
	}

	got := ExpandCategoryPermissions(nil, channel)

	if len(got.Overwrites) != 1 {
		t.Fatalf("expected only the channel's own overwrite to remain, got %+v", got.Overwrites)
	}
	ow := got.Overwrites["role-a"]
	if len(ow.Allow) != 1 || ow.Allow[0] != PermSendMessages {
		t.Errorf("role-a overwrite = %+v, want Allow=[SEND_MESSAGES]", ow)
	}
}
