package main

import (
	"bytes"
	"log/slog"
	"testing"

	"github.com/bwmarrin/discordgo"
)

func testGatewayLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
}

// categoriesFromState must order categories by their own Discord Position
// (sidebar display order), not by id -- a prior version sorted by id, which
// bore no relation to how Discord actually displays them.
func TestCategoriesFromState_OrdersByCategoryPosition(t *testing.T) {
	session := &discordgo.Session{State: discordgo.NewState()}
	guild := &discordgo.Guild{ID: "guild1"}
	if err := session.State.GuildAdd(guild); err != nil {
		t.Fatalf("GuildAdd: %v", err)
	}

	channels := []*discordgo.Channel{
		{ID: "cat-z", GuildID: "guild1", Type: discordgo.ChannelTypeGuildCategory, Name: "Z-Category", Position: 5},
		{ID: "cat-a", GuildID: "guild1", Type: discordgo.ChannelTypeGuildCategory, Name: "A-Category", Position: 1},
		{ID: "chan1", GuildID: "guild1", Type: discordgo.ChannelTypeGuildText, Name: "general", ParentID: "cat-a", Position: 0},
		{ID: "chan2", GuildID: "guild1", Type: discordgo.ChannelTypeGuildText, Name: "voice-chat", ParentID: "cat-z", Position: 0},
	}
	for _, ch := range channels {
		if err := session.State.ChannelAdd(ch); err != nil {
			t.Fatalf("ChannelAdd: %v", err)
		}
	}

	cats := categoriesFromState(session, "guild1", testGatewayLogger())

	if len(cats) != 2 {
		t.Fatalf("cats = %+v, want 2", cats)
	}
	// cat-a has the lower Position (1 < 5), so it must sort first despite
	// its id sorting after cat-z lexically.
	if cats[0].ID != "cat-a" || cats[1].ID != "cat-z" {
		t.Fatalf("order = [%s, %s], want [cat-a, cat-z] (must follow Discord Position, not id)", cats[0].ID, cats[1].ID)
	}
}
