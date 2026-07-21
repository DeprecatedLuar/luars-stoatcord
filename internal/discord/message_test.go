package discord

import (
	"bytes"
	"context"
	"testing"

	"github.com/bwmarrin/discordgo"
	"github.com/luar/stoatcord/internal/canonical"
	"github.com/luar/stoatcord/internal/engine"
)

type fakeMessageWriter struct {
	sendChannelID string
	sendMsg       canonical.StoatMessage
	sendReturns   string

	editChannelID, editMessageID, editContent string

	deleteChannelID, deleteMessageID string
}

func (f *fakeMessageWriter) SendMessage(ctx context.Context, channelID string, msg canonical.StoatMessage) (string, error) {
	f.sendChannelID = channelID
	f.sendMsg = msg
	return f.sendReturns, nil
}

func (f *fakeMessageWriter) EditMessage(ctx context.Context, channelID, messageID, content string) error {
	f.editChannelID, f.editMessageID, f.editContent = channelID, messageID, content
	return nil
}

func (f *fakeMessageWriter) DeleteMessage(ctx context.Context, channelID, messageID string) error {
	f.deleteChannelID, f.deleteMessageID = channelID, messageID
	return nil
}

func TestBuildMessageOp_SkipsEmptyContent(t *testing.T) {
	var buf bytes.Buffer
	logger := newTestLogger(&buf)
	session := &discordgo.Session{State: discordgo.NewState()}

	m := &discordgo.Message{ID: "msg1", ChannelID: "chan1", Content: "", Author: &discordgo.User{ID: "u1", Username: "alice"}}

	_, ok := BuildMessageOp(engine.OpCreate, session, "guild1", m, newFakeMappingReader(), &fakeMessageWriter{}, logger)
	if ok {
		t.Fatal("expected ok=false for empty content")
	}
}

func TestBuildMessageOp_SetsChannelIDAndDependsOn(t *testing.T) {
	var buf bytes.Buffer
	logger := newTestLogger(&buf)
	session := &discordgo.Session{State: discordgo.NewState()}

	m := &discordgo.Message{ID: "msg1", ChannelID: "chan1", Content: "hello", Author: &discordgo.User{ID: "u1", Username: "alice"}}

	op, ok := BuildMessageOp(engine.OpCreate, session, "guild1", m, newFakeMappingReader(), &fakeMessageWriter{}, logger)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if op.ChannelID != "chan1" {
		t.Fatalf("op.ChannelID = %q, want chan1", op.ChannelID)
	}
	if len(op.DependsOn) != 1 || op.DependsOn[0] != (engine.DependencyKey{EntityType: engine.EntityChannel, DiscordID: "chan1"}) {
		t.Fatalf("DependsOn = %+v, want [{channel chan1}]", op.DependsOn)
	}
}

func TestBuildMessageOp_ApplyCreatesWhenNotMapped(t *testing.T) {
	var buf bytes.Buffer
	logger := newTestLogger(&buf)
	session := &discordgo.Session{State: discordgo.NewState()}
	m := &discordgo.Message{ID: "msg1", ChannelID: "chan1", Content: "hello", Author: &discordgo.User{ID: "u1", Username: "alice"}}

	mappings := newFakeMappingReader()
	mappings.set(string(engine.EntityChannel), "chan1", engine.Mapping{Found: true, StoatID: "stoat-chan1", Status: engine.StatusActive})

	writer := &fakeMessageWriter{sendReturns: "stoat-msg1"}
	op, ok := BuildMessageOp(engine.OpCreate, session, "guild1", m, mappings, writer, logger)
	if !ok {
		t.Fatal("expected ok=true")
	}

	id, err := op.Apply(context.Background())
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if id != "stoat-msg1" {
		t.Fatalf("id = %q, want stoat-msg1", id)
	}
	if writer.sendChannelID != "stoat-chan1" || writer.sendMsg.Content != "hello" {
		t.Fatalf("SendMessage got channelID=%q msg=%+v", writer.sendChannelID, writer.sendMsg)
	}
}

func TestBuildMessageOp_ApplyEditsWhenAlreadyMapped(t *testing.T) {
	var buf bytes.Buffer
	logger := newTestLogger(&buf)
	session := &discordgo.Session{State: discordgo.NewState()}
	m := &discordgo.Message{ID: "msg1", ChannelID: "chan1", Content: "edited", Author: &discordgo.User{ID: "u1", Username: "alice"}}

	mappings := newFakeMappingReader()
	mappings.set(string(engine.EntityChannel), "chan1", engine.Mapping{Found: true, StoatID: "stoat-chan1", Status: engine.StatusActive})
	mappings.set(string(engine.EntityMessage), "msg1", engine.Mapping{Found: true, StoatID: "stoat-msg1", Status: engine.StatusActive})

	writer := &fakeMessageWriter{}
	op, ok := BuildMessageOp(engine.OpUpdate, session, "guild1", m, mappings, writer, logger)
	if !ok {
		t.Fatal("expected ok=true")
	}

	if _, err := op.Apply(context.Background()); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if writer.editChannelID != "stoat-chan1" || writer.editMessageID != "stoat-msg1" || writer.editContent != "edited" {
		t.Fatalf("EditMessage got channelID=%q messageID=%q content=%q", writer.editChannelID, writer.editMessageID, writer.editContent)
	}
	if writer.sendChannelID != "" {
		t.Fatal("SendMessage should not have been called for an already-mapped message")
	}
}

func TestBuildMessageOp_DiffComparesContentOnly(t *testing.T) {
	var buf bytes.Buffer
	logger := newTestLogger(&buf)
	session := &discordgo.Session{State: discordgo.NewState()}
	m := &discordgo.Message{ID: "msg1", ChannelID: "chan1", Content: "same", Author: &discordgo.User{ID: "u1", Username: "alice"}}

	op, ok := BuildMessageOp(engine.OpUpdate, session, "guild1", m, newFakeMappingReader(), &fakeMessageWriter{}, logger)
	if !ok {
		t.Fatal("expected ok=true")
	}

	stored := canonical.Message{ID: "msg1", ChannelID: "chan1", Content: "same", AuthorName: "totally-different-author"}
	storedJSON, err := stored.CanonicalJSON()
	if err != nil {
		t.Fatalf("CanonicalJSON: %v", err)
	}

	equal, err := op.Diff(string(storedJSON))
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if !equal {
		t.Fatal("Diff should report equal when Content matches, ignoring divergent author fields")
	}

	changed := canonical.Message{ID: "msg1", ChannelID: "chan1", Content: "different"}
	changedJSON, err := changed.CanonicalJSON()
	if err != nil {
		t.Fatalf("CanonicalJSON: %v", err)
	}
	equal, err = op.Diff(string(changedJSON))
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if equal {
		t.Fatal("Diff should report not-equal when Content differs")
	}
}

func TestBuildMessageDeleteOp_ApplyDeletesUsingMappedStoatIDs(t *testing.T) {
	mappings := newFakeMappingReader()
	mappings.set(string(engine.EntityChannel), "chan1", engine.Mapping{Found: true, StoatID: "stoat-chan1", Status: engine.StatusActive})
	mappings.set(string(engine.EntityMessage), "msg1", engine.Mapping{Found: true, StoatID: "stoat-msg1", Status: engine.StatusActive})

	writer := &fakeMessageWriter{}
	op := BuildMessageDeleteOp("msg1", "chan1", mappings, writer)

	if op.Kind != engine.OpDelete || op.EntityType != engine.EntityMessage || op.DiscordID != "msg1" || op.ChannelID != "chan1" {
		t.Fatalf("op = %+v", op)
	}

	if _, err := op.Apply(context.Background()); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if writer.deleteChannelID != "stoat-chan1" || writer.deleteMessageID != "stoat-msg1" {
		t.Fatalf("DeleteMessage got channelID=%q messageID=%q", writer.deleteChannelID, writer.deleteMessageID)
	}
}

func TestBuildMessageDeleteOp_ApplyNoOpWhenNotMapped(t *testing.T) {
	writer := &fakeMessageWriter{}
	op := BuildMessageDeleteOp("msg1", "chan1", newFakeMappingReader(), writer)

	if _, err := op.Apply(context.Background()); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if writer.deleteMessageID != "" {
		t.Fatalf("DeleteMessage called with messageID=%q, want never called", writer.deleteMessageID)
	}
}

func TestAuthorDisplayFromDiscord_PrefersNicknameAndTopColouredRole(t *testing.T) {
	session := &discordgo.Session{State: discordgo.NewState()}
	if err := session.State.GuildAdd(&discordgo.Guild{
		ID: "guild1",
		Roles: []*discordgo.Role{
			{ID: "role-low", Name: "low", Color: 0x00FF00, Position: 1},
			{ID: "role-high", Name: "high", Color: 0xFF00AA, Position: 2},
			{ID: "role-nocolor", Name: "nocolor", Color: 0, Position: 3},
		},
	}); err != nil {
		t.Fatalf("GuildAdd: %v", err)
	}

	var buf bytes.Buffer
	logger := newTestLogger(&buf)
	m := &discordgo.Message{
		Author: &discordgo.User{ID: "u1", Username: "alice", Avatar: "abc123"},
		Member: &discordgo.Member{
			Nick:  "Ali",
			Roles: []string{"role-low", "role-high", "role-nocolor"},
		},
	}

	name, avatar, colour := authorDisplayFromDiscord(session, "guild1", m, logger)
	if name != "Ali" {
		t.Fatalf("name = %q, want Ali", name)
	}
	if avatar == "" {
		t.Fatal("expected a non-empty avatar URL")
	}
	if colour != "#FF00AA" {
		t.Fatalf("colour = %q, want #FF00AA (highest-position non-zero-colour role)", colour)
	}
}

func TestAuthorDisplayFromDiscord_FallsBackToUsernameWhenMemberNil(t *testing.T) {
	session := &discordgo.Session{State: discordgo.NewState()}
	var buf bytes.Buffer
	logger := newTestLogger(&buf)
	m := &discordgo.Message{
		Author: &discordgo.User{ID: "u1", Username: "alice"},
		Member: nil,
	}

	name, _, colour := authorDisplayFromDiscord(session, "guild1", m, logger)
	if name != "alice" {
		t.Fatalf("name = %q, want alice", name)
	}
	if colour != "" {
		t.Fatalf("colour = %q, want empty", colour)
	}
}
