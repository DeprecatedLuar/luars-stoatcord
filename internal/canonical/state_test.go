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
