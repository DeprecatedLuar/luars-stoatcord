package main

import (
	"context"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/luar/stoatcord/internal/stoat"
)

func TestWaitForGuildReady_ReturnsImmediatelyWhenAlreadyInState(t *testing.T) {
	session := &discordgo.Session{State: discordgo.NewState()}
	if err := session.State.GuildAdd(&discordgo.Guild{ID: "guild1"}); err != nil {
		t.Fatalf("GuildAdd: %v", err)
	}

	start := time.Now()
	err := waitForGuildReady(context.Background(), session, "guild1", 5*time.Second)
	if err != nil {
		t.Fatalf("waitForGuildReady: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Fatalf("waitForGuildReady took %v, want near-instant for an already-ready guild", elapsed)
	}
}

// A guild that never sends GUILD_CREATE (e.g. bot not actually a member,
// misconfigured guild id) must not wedge startup forever.
func TestWaitForGuildReady_TimesOutWhenGuildNeverArrives(t *testing.T) {
	session := &discordgo.Session{State: discordgo.NewState()}

	start := time.Now()
	err := waitForGuildReady(context.Background(), session, "never-arrives", 50*time.Millisecond)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("waitForGuildReady returned nil, want a timeout error")
	}
	if elapsed > 500*time.Millisecond {
		t.Fatalf("waitForGuildReady took %v to give up, want close to the 50ms timeout", elapsed)
	}
}

func TestWaitForStoatHealthy_TimesOutWhenNeverHealthy(t *testing.T) {
	gw := stoat.NewGateway(testGatewayLogger())

	start := time.Now()
	err := waitForStoatHealthy(context.Background(), gw, 50*time.Millisecond)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("waitForStoatHealthy returned nil, want a timeout error")
	}
	if elapsed > 500*time.Millisecond {
		t.Fatalf("waitForStoatHealthy took %v to give up, want close to the 50ms timeout", elapsed)
	}
}
