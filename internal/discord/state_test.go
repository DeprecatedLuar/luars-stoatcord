package discord

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
)

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

	cats := categoriesFromState(session, "guild1", newTestLogger(&bytes.Buffer{}))

	if len(cats) != 2 {
		t.Fatalf("cats = %+v, want 2", cats)
	}
	// cat-a has the lower Position (1 < 5), so it must sort first despite
	// its id sorting after cat-z lexically.
	if cats[0].ID != "cat-a" || cats[1].ID != "cat-z" {
		t.Fatalf("order = [%s, %s], want [cat-a, cat-z] (must follow Discord Position, not id)", cats[0].ID, cats[1].ID)
	}
	// canonical.Category.Position must be the sorted index (0, 1, ...), not
	// the raw Discord Position integer -- otherwise the gap between 1 and 5
	// would leak into canonical state for no reason.
	if cats[0].Position != 0 || cats[1].Position != 1 {
		t.Fatalf("positions = [%d, %d], want [0, 1] (sorted index, not raw Discord Position)", cats[0].Position, cats[1].Position)
	}
}

func TestWaitForGuildReady_ReturnsImmediatelyWhenAlreadyInState(t *testing.T) {
	session := &discordgo.Session{State: discordgo.NewState()}
	if err := session.State.GuildAdd(&discordgo.Guild{ID: "guild1"}); err != nil {
		t.Fatalf("GuildAdd: %v", err)
	}

	start := time.Now()
	err := WaitForGuildReady(context.Background(), session, "guild1", 5*time.Second)
	if err != nil {
		t.Fatalf("WaitForGuildReady: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Fatalf("WaitForGuildReady took %v, want near-instant for an already-ready guild", elapsed)
	}
}

// A guild that never sends GUILD_CREATE (e.g. bot not actually a member,
// misconfigured guild id) must not wedge startup forever.
func TestWaitForGuildReady_TimesOutWhenGuildNeverArrives(t *testing.T) {
	session := &discordgo.Session{State: discordgo.NewState()}

	start := time.Now()
	err := WaitForGuildReady(context.Background(), session, "never-arrives", 50*time.Millisecond)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("WaitForGuildReady returned nil, want a timeout error")
	}
	if elapsed > 500*time.Millisecond {
		t.Fatalf("WaitForGuildReady took %v to give up, want close to the 50ms timeout", elapsed)
	}
}
