package canonical

import "testing"

func TestChannel_CanonicalJSON_SortsRoleKeysRegardlessOfInsertionOrder(t *testing.T) {
	a := Channel{
		ID:   "c1",
		Name: "general",
		Overwrites: map[string]Overwrite{
			"role-b": {Allow: []Permission{PermViewChannel}},
			"role-a": {Allow: []Permission{PermSendMessages}},
		},
	}
	b := Channel{
		ID:   "c1",
		Name: "general",
		Overwrites: map[string]Overwrite{
			"role-a": {Allow: []Permission{PermSendMessages}},
			"role-b": {Allow: []Permission{PermViewChannel}},
		},
	}

	jsonA, err := a.CanonicalJSON()
	if err != nil {
		t.Fatalf("a.CanonicalJSON() error: %v", err)
	}
	jsonB, err := b.CanonicalJSON()
	if err != nil {
		t.Fatalf("b.CanonicalJSON() error: %v", err)
	}

	if string(jsonA) != string(jsonB) {
		t.Errorf("CanonicalJSON differs by map insertion order:\na = %s\nb = %s", jsonA, jsonB)
	}
}

func TestChannel_CanonicalJSON_SortsPermissionsWithinOverwrite(t *testing.T) {
	a := Channel{
		ID:   "c1",
		Name: "general",
		Overwrites: map[string]Overwrite{
			"role-a": {Allow: []Permission{PermSendMessages, PermViewChannel}},
		},
	}
	b := Channel{
		ID:   "c1",
		Name: "general",
		Overwrites: map[string]Overwrite{
			"role-a": {Allow: []Permission{PermViewChannel, PermSendMessages}},
		},
	}

	jsonA, err := a.CanonicalJSON()
	if err != nil {
		t.Fatalf("a.CanonicalJSON() error: %v", err)
	}
	jsonB, err := b.CanonicalJSON()
	if err != nil {
		t.Fatalf("b.CanonicalJSON() error: %v", err)
	}

	if string(jsonA) != string(jsonB) {
		t.Errorf("CanonicalJSON differs by permission slice order:\na = %s\nb = %s", jsonA, jsonB)
	}
}

func TestParseChannelCanonicalJSON_RoundTripsCanonicalJSON(t *testing.T) {
	c := Channel{
		ID:         "c1",
		Name:       "general",
		Type:       ChannelTypeVoice,
		CategoryID: "cat1",
		Position:   3,
		Overwrites: map[string]Overwrite{
			"role-a": {Allow: []Permission{PermViewChannel}, Deny: []Permission{PermSendMessages}},
		},
	}

	data, err := c.CanonicalJSON()
	if err != nil {
		t.Fatalf("CanonicalJSON() error: %v", err)
	}

	got, err := ParseChannelCanonicalJSON(data)
	if err != nil {
		t.Fatalf("ParseChannelCanonicalJSON() error: %v", err)
	}

	if got.ID != c.ID || got.Name != c.Name || got.Type != c.Type || got.CategoryID != c.CategoryID || got.Position != c.Position {
		t.Fatalf("got %+v, want fields matching %+v", got, c)
	}
	ow, ok := got.Overwrites["role-a"]
	if !ok {
		t.Fatalf("Overwrites missing role-a: %+v", got.Overwrites)
	}
	if len(ow.Allow) != 1 || ow.Allow[0] != PermViewChannel || len(ow.Deny) != 1 || ow.Deny[0] != PermSendMessages {
		t.Fatalf("Overwrites[role-a] = %+v", ow)
	}
}

func TestParseRoleCanonicalJSON_RoundTripsCanonicalJSON(t *testing.T) {
	r := Role{
		ID:     "role-1",
		Name:   "Moderator",
		Colour: "#FF00AA",
		Hoist:  true,
		Rank:   5,
		Permissions: Overwrite{
			Allow: []Permission{PermViewChannel},
			Deny:  []Permission{PermSendMessages},
		},
	}

	data, err := r.CanonicalJSON()
	if err != nil {
		t.Fatalf("CanonicalJSON() error: %v", err)
	}

	got, err := ParseRoleCanonicalJSON(data)
	if err != nil {
		t.Fatalf("ParseRoleCanonicalJSON() error: %v", err)
	}

	if got.ID != r.ID || got.Name != r.Name || got.Colour != r.Colour || got.Hoist != r.Hoist || got.Rank != r.Rank {
		t.Fatalf("got %+v, want fields matching %+v", got, r)
	}
	if len(got.Permissions.Allow) != 1 || got.Permissions.Allow[0] != PermViewChannel {
		t.Fatalf("Permissions.Allow = %+v", got.Permissions.Allow)
	}
	if len(got.Permissions.Deny) != 1 || got.Permissions.Deny[0] != PermSendMessages {
		t.Fatalf("Permissions.Deny = %+v", got.Permissions.Deny)
	}
}

func TestRole_CanonicalJSON_SortsPermissions(t *testing.T) {
	a := Role{ID: "r1", Permissions: Overwrite{Allow: []Permission{PermSendMessages, PermViewChannel}}}
	b := Role{ID: "r1", Permissions: Overwrite{Allow: []Permission{PermViewChannel, PermSendMessages}}}

	jsonA, err := a.CanonicalJSON()
	if err != nil {
		t.Fatalf("CanonicalJSON() error: %v", err)
	}
	jsonB, err := b.CanonicalJSON()
	if err != nil {
		t.Fatalf("CanonicalJSON() error: %v", err)
	}
	if string(jsonA) != string(jsonB) {
		t.Errorf("CanonicalJSON differs by permission slice order:\na = %s\nb = %s", jsonA, jsonB)
	}
}

func TestChannel_CanonicalJSON_NilOverwritesProducesEmptyObject(t *testing.T) {
	c := Channel{ID: "c1", Name: "general"}

	got, err := c.CanonicalJSON()
	if err != nil {
		t.Fatalf("CanonicalJSON() error: %v", err)
	}

	const want = `{"id":"c1","name":"general","type":"","category_id":"","position":0,"overwrites":{}}`
	if string(got) != want {
		t.Errorf("CanonicalJSON() = %s, want %s", got, want)
	}
}
