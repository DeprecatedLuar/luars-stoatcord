package discord

import (
	"bytes"
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"
	"github.com/luar/stoatcord/internal/canonical"
	"github.com/luar/stoatcord/internal/engine"
)

func TestIsGifLink_MatchesKnownHosts(t *testing.T) {
	tests := []struct {
		name    string
		content string
		wantURL string
	}{
		{"tenor", "https://tenor.com/view/hatsune-miku-crying-gif-123", "https://tenor.com/view/hatsune-miku-crying-gif-123"},
		{"giphy", "https://giphy.com/gifs/some-slug-abc123", "https://giphy.com/gifs/some-slug-abc123"},
		{"klipy", "https://klipy.com/gifs/hatsune-miku-crying-2", "https://klipy.com/gifs/hatsune-miku-crying-2"},
		{"gfycat", "https://gfycat.com/somegif", "https://gfycat.com/somegif"},
		{"redgifs", "https://redgifs.com/watch/somegif", "https://redgifs.com/watch/somegif"},
		{"gifbox", "https://gifbox.me/view/abc123", "https://gifbox.me/view/abc123"},
		{"yiffbox", "https://yiffbox.me/view/abc123", "https://yiffbox.me/view/abc123"},
		{"www prefix", "https://www.tenor.com/view/abc-123", "https://www.tenor.com/view/abc-123"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url, ok := isGifLink(tt.content)
			if !ok {
				t.Fatalf("isGifLink(%q) ok=false, want true", tt.content)
			}
			if url != tt.wantURL {
				t.Fatalf("isGifLink(%q) url=%q, want %q", tt.content, url, tt.wantURL)
			}
		})
	}
}

func TestIsGifLink_NoMatchOnUnknownHost(t *testing.T) {
	url, ok := isGifLink("https://example.com/cat.gif")
	if ok {
		t.Fatalf("isGifLink matched unknown host, url=%q", url)
	}
}

func TestIsGifLink_NoMatchOnPlainText(t *testing.T) {
	url, ok := isGifLink("just saying hello, no links here")
	if ok {
		t.Fatalf("isGifLink matched plain text, url=%q", url)
	}
}

func TestIsGifLink_MatchesWithinMixedContent(t *testing.T) {
	url, ok := isGifLink("check this out https://tenor.com/view/funny-123 lol")
	if !ok {
		t.Fatal("expected a match within mixed content")
	}
	if url != "https://tenor.com/view/funny-123" {
		t.Fatalf("url = %q, want https://tenor.com/view/funny-123 (trailing text excluded)", url)
	}
}

func storedMessageMapping(t *testing.T, content string) engine.Mapping {
	t.Helper()
	msg := canonical.Message{ID: "msg1", ChannelID: "chan1", Content: content}
	state, err := msg.CanonicalJSON()
	if err != nil {
		t.Fatalf("CanonicalJSON: %v", err)
	}
	return engine.Mapping{Found: true, StoatID: "stoat-msg1", Status: engine.StatusActive, CanonicalState: string(state)}
}

func TestLogGifDetectionMismatch_LogsWhenHeuristicMissed(t *testing.T) {
	var buf bytes.Buffer
	logger := newTestLogger(&buf)

	mappings := newFakeMappingReader()
	mappings.set(string(engine.EntityMessage), "msg1", storedMessageMapping(t, "https://example.com/cat.gif"))

	m := &discordgo.Message{
		ID: "msg1",
		Embeds: []*discordgo.MessageEmbed{
			{Type: discordgo.EmbedTypeGifv, URL: "https://example.com/cat.gif"},
		},
	}

	logGifDetectionMismatch(m, mappings, logger)

	if !strings.Contains(buf.String(), "msg1") || !strings.Contains(buf.String(), "gifv") {
		t.Fatalf("expected a logged mismatch mentioning msg1 and gifv, got: %s", buf.String())
	}
}

func TestLogGifDetectionMismatch_NoLogWhenHeuristicAlreadyMatched(t *testing.T) {
	var buf bytes.Buffer
	logger := newTestLogger(&buf)

	mappings := newFakeMappingReader()
	mappings.set(string(engine.EntityMessage), "msg1", storedMessageMapping(t, "https://tenor.com/view/funny-123"))

	m := &discordgo.Message{
		ID: "msg1",
		Embeds: []*discordgo.MessageEmbed{
			{Type: discordgo.EmbedTypeGifv, URL: "https://tenor.com/view/funny-123"},
		},
	}

	logGifDetectionMismatch(m, mappings, logger)

	if buf.Len() != 0 {
		t.Fatalf("expected no log output, got: %s", buf.String())
	}
}

func TestLogGifDetectionMismatch_NoLogWhenNoMediaEmbed(t *testing.T) {
	var buf bytes.Buffer
	logger := newTestLogger(&buf)

	mappings := newFakeMappingReader()
	mappings.set(string(engine.EntityMessage), "msg1", storedMessageMapping(t, "https://example.com/cat.gif"))

	m := &discordgo.Message{
		ID:     "msg1",
		Embeds: []*discordgo.MessageEmbed{{Type: discordgo.EmbedTypeArticle}},
	}

	logGifDetectionMismatch(m, mappings, logger)

	if buf.Len() != 0 {
		t.Fatalf("expected no log output for a non-media embed, got: %s", buf.String())
	}
}

func TestLogGifDetectionMismatch_NoLogWhenMessageNotMapped(t *testing.T) {
	var buf bytes.Buffer
	logger := newTestLogger(&buf)

	mappings := newFakeMappingReader()

	m := &discordgo.Message{
		ID:     "msg1",
		Embeds: []*discordgo.MessageEmbed{{Type: discordgo.EmbedTypeGifv, URL: "https://example.com/cat.gif"}},
	}

	logGifDetectionMismatch(m, mappings, logger)

	if buf.Len() != 0 {
		t.Fatalf("expected no log output when the message has no stored mapping, got: %s", buf.String())
	}
}
