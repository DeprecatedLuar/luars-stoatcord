package canonical

import (
	"bytes"
	"testing"
)

func TestRole_ToStoat_TranslatesFieldsAndPermissions(t *testing.T) {
	var buf bytes.Buffer
	logger := newTestLogger(&buf)
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

	got := r.ToStoat(logger)

	if got.Name != "Moderator" || got.Colour != "#FF00AA" || !got.Hoist || got.Rank != 5 {
		t.Errorf("ToStoat() = %+v, unexpected fields", got)
	}
	wantAllow := uint64(1) << 20
	wantDeny := uint64(1) << 22
	if got.Permissions.Allow != wantAllow || got.Permissions.Deny != wantDeny {
		t.Errorf("Permissions = %+v, want Allow=%d Deny=%d", got.Permissions, wantAllow, wantDeny)
	}
}

func TestEmoji_ToStoat_CopiesFields(t *testing.T) {
	e := Emoji{ID: "e1", Name: "pog", Animated: true, NSFW: false}

	got := e.ToStoat()

	if got.Name != "pog" || !got.Animated || got.NSFW {
		t.Errorf("ToStoat() = %+v, unexpected fields", got)
	}
}

func TestServer_ToStoat_CopiesFields(t *testing.T) {
	s := Server{ID: "s1", Name: "My Server", Description: "desc", IconRef: "icon", BannerRef: "banner"}

	got := s.ToStoat()

	if got.Name != "My Server" || got.Description != "desc" || got.IconRef != "icon" || got.BannerRef != "banner" {
		t.Errorf("ToStoat() = %+v, unexpected fields", got)
	}
}

func TestMessage_ToStoat_BuildsMasquerade(t *testing.T) {
	m := Message{
		ID:              "m1",
		ChannelID:       "c1",
		AuthorName:      "Alice",
		AuthorAvatarRef: "avatar-url",
		AuthorColour:    "#123456",
		Content:         "hello",
		AttachmentRefs:  []string{"att-1", "att-2"},
	}

	got := m.ToStoat()

	if got.Content != "hello" {
		t.Errorf("Content = %q, want hello", got.Content)
	}
	if got.Masquerade.Name != "Alice" || got.Masquerade.Avatar != "avatar-url" || got.Masquerade.Colour != "#123456" {
		t.Errorf("Masquerade = %+v, unexpected fields", got.Masquerade)
	}
	if len(got.Attachments) != 2 || got.Attachments[0] != "att-1" || got.Attachments[1] != "att-2" {
		t.Errorf("Attachments = %v, want [att-1 att-2]", got.Attachments)
	}
}

func TestCategory_CanonicalJSON_PreservesChannelOrder(t *testing.T) {
	c := Category{ID: "cat-1", Name: "General", ChannelIDs: []string{"c3", "c1", "c2"}}

	got, err := c.CanonicalJSON()
	if err != nil {
		t.Fatalf("CanonicalJSON() error: %v", err)
	}

	const want = `{"id":"cat-1","name":"General","channel_ids":["c3","c1","c2"]}`
	if string(got) != want {
		t.Errorf("CanonicalJSON() = %s, want %s", got, want)
	}
}
