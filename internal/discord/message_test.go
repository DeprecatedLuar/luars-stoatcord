package discord

import (
	"bytes"
	"context"
	"fmt"
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

	uploadURLs []string
	uploadErr  error
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

func (f *fakeMessageWriter) UploadFromURL(ctx context.Context, tag, url string) (string, error) {
	if f.uploadErr != nil {
		return "", f.uploadErr
	}
	f.uploadURLs = append(f.uploadURLs, url)
	return "autumn-" + url, nil
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

func TestBuildMessageOp_AllowsEmptyContentCreateWithAttachments(t *testing.T) {
	var buf bytes.Buffer
	logger := newTestLogger(&buf)
	session := &discordgo.Session{State: discordgo.NewState()}

	m := &discordgo.Message{
		ID: "msg1", ChannelID: "chan1", Content: "",
		Author:      &discordgo.User{ID: "u1", Username: "alice"},
		Attachments: []*discordgo.MessageAttachment{{ID: "att1", URL: "https://cdn.discordapp.com/attachments/1/2/photo.png"}},
	}

	_, ok := BuildMessageOp(engine.OpCreate, session, "guild1", m, newFakeMappingReader(), &fakeMessageWriter{}, logger)
	if !ok {
		t.Fatal("expected ok=true for attachment-only create")
	}
}

func TestBuildMessageOp_StillSkipsEmptyContentUpdateWithAttachments(t *testing.T) {
	var buf bytes.Buffer
	logger := newTestLogger(&buf)
	session := &discordgo.Session{State: discordgo.NewState()}

	m := &discordgo.Message{
		ID: "msg1", ChannelID: "chan1", Content: "",
		Author:      &discordgo.User{ID: "u1", Username: "alice"},
		Attachments: []*discordgo.MessageAttachment{{ID: "att1", URL: "https://cdn.discordapp.com/attachments/1/2/photo.png"}},
	}

	_, ok := BuildMessageOp(engine.OpUpdate, session, "guild1", m, newFakeMappingReader(), &fakeMessageWriter{}, logger)
	if ok {
		t.Fatal("expected ok=false: an update's empty content is always Discord's partial-payload case, regardless of attachments")
	}
}

func TestBuildMessageOp_ApplyUploadsAttachmentsBeforeSend(t *testing.T) {
	var buf bytes.Buffer
	logger := newTestLogger(&buf)
	session := &discordgo.Session{State: discordgo.NewState()}
	m := &discordgo.Message{
		ID: "msg1", ChannelID: "chan1", Content: "look at this",
		Author: &discordgo.User{ID: "u1", Username: "alice"},
		Attachments: []*discordgo.MessageAttachment{
			{ID: "att1", URL: "https://cdn.discordapp.com/attachments/1/2/photo.png"},
			{ID: "att2", URL: "https://cdn.discordapp.com/attachments/1/3/photo2.png"},
		},
	}

	mappings := newFakeMappingReader()
	mappings.set(string(engine.EntityChannel), "chan1", engine.Mapping{Found: true, StoatID: "stoat-chan1", Status: engine.StatusActive})

	writer := &fakeMessageWriter{sendReturns: "stoat-msg1"}
	op, ok := BuildMessageOp(engine.OpCreate, session, "guild1", m, mappings, writer, logger)
	if !ok {
		t.Fatal("expected ok=true")
	}

	if _, err := op.Apply(context.Background()); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	wantURLs := []string{"https://cdn.discordapp.com/attachments/1/2/photo.png", "https://cdn.discordapp.com/attachments/1/3/photo2.png"}
	if len(writer.uploadURLs) != 2 || writer.uploadURLs[0] != wantURLs[0] || writer.uploadURLs[1] != wantURLs[1] {
		t.Fatalf("uploadURLs = %v, want %v", writer.uploadURLs, wantURLs)
	}
	wantAttachments := []string{"autumn-" + wantURLs[0], "autumn-" + wantURLs[1]}
	if len(writer.sendMsg.Attachments) != 2 || writer.sendMsg.Attachments[0] != wantAttachments[0] || writer.sendMsg.Attachments[1] != wantAttachments[1] {
		t.Fatalf("sendMsg.Attachments = %v, want %v (uploaded Autumn ids, not raw URLs)", writer.sendMsg.Attachments, wantAttachments)
	}
}

func TestBuildMessageOp_ApplyPropagatesAttachmentUploadError(t *testing.T) {
	var buf bytes.Buffer
	logger := newTestLogger(&buf)
	session := &discordgo.Session{State: discordgo.NewState()}
	m := &discordgo.Message{
		ID: "msg1", ChannelID: "chan1", Content: "look at this",
		Author:      &discordgo.User{ID: "u1", Username: "alice"},
		Attachments: []*discordgo.MessageAttachment{{ID: "att1", URL: "https://cdn.discordapp.com/attachments/1/2/photo.png"}},
	}

	mappings := newFakeMappingReader()
	mappings.set(string(engine.EntityChannel), "chan1", engine.Mapping{Found: true, StoatID: "stoat-chan1", Status: engine.StatusActive})

	writer := &fakeMessageWriter{uploadErr: fmt.Errorf("autumn unavailable")}
	op, ok := BuildMessageOp(engine.OpCreate, session, "guild1", m, mappings, writer, logger)
	if !ok {
		t.Fatal("expected ok=true")
	}

	if _, err := op.Apply(context.Background()); err == nil {
		t.Fatal("expected Apply to propagate the attachment upload error")
	}
	if writer.sendChannelID != "" {
		t.Fatal("SendMessage should not have been called when an attachment upload fails")
	}
}

func TestBuildMessageOp_DropsAttachmentsBeyondStoatCap(t *testing.T) {
	var buf bytes.Buffer
	logger := newTestLogger(&buf)
	session := &discordgo.Session{State: discordgo.NewState()}

	var attachments []*discordgo.MessageAttachment
	for i := range maxStoatAttachmentsPerMessage + 2 {
		attachments = append(attachments, &discordgo.MessageAttachment{ID: fmt.Sprintf("att%d", i), URL: fmt.Sprintf("https://cdn.discordapp.com/attachments/1/%d/f.png", i)})
	}
	m := &discordgo.Message{ID: "msg1", ChannelID: "chan1", Content: "many files", Author: &discordgo.User{ID: "u1", Username: "alice"}, Attachments: attachments}

	mappings := newFakeMappingReader()
	mappings.set(string(engine.EntityChannel), "chan1", engine.Mapping{Found: true, StoatID: "stoat-chan1", Status: engine.StatusActive})

	writer := &fakeMessageWriter{sendReturns: "stoat-msg1"}
	op, ok := BuildMessageOp(engine.OpCreate, session, "guild1", m, mappings, writer, logger)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if _, err := op.Apply(context.Background()); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(writer.uploadURLs) != maxStoatAttachmentsPerMessage {
		t.Fatalf("uploaded %d attachments, want %d (cap)", len(writer.uploadURLs), maxStoatAttachmentsPerMessage)
	}
}

func TestBuildMessageOp_GifLinkOnlyContentBecomesAttachment(t *testing.T) {
	var buf bytes.Buffer
	logger := newTestLogger(&buf)
	session := &discordgo.Session{State: discordgo.NewState()}
	m := &discordgo.Message{
		ID: "msg1", ChannelID: "chan1", Content: "https://tenor.com/view/funny-123",
		Author: &discordgo.User{ID: "u1", Username: "alice"},
	}

	mappings := newFakeMappingReader()
	mappings.set(string(engine.EntityChannel), "chan1", engine.Mapping{Found: true, StoatID: "stoat-chan1", Status: engine.StatusActive})

	writer := &fakeMessageWriter{sendReturns: "stoat-msg1"}
	op, ok := BuildMessageOp(engine.OpCreate, session, "guild1", m, mappings, writer, logger)
	if !ok {
		t.Fatal("expected ok=true")
	}

	if _, err := op.Apply(context.Background()); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if len(writer.uploadURLs) != 1 || writer.uploadURLs[0] != "https://tenor.com/view/funny-123" {
		t.Fatalf("uploadURLs = %v, want the gif link uploaded", writer.uploadURLs)
	}
	if writer.sendMsg.Content != "" {
		t.Fatalf("sendMsg.Content = %q, want empty (URL-only content stripped)", writer.sendMsg.Content)
	}
	if len(writer.sendMsg.Attachments) != 1 || writer.sendMsg.Attachments[0] != "autumn-https://tenor.com/view/funny-123" {
		t.Fatalf("sendMsg.Attachments = %v, want the uploaded gif attachment", writer.sendMsg.Attachments)
	}
}

func TestBuildMessageOp_GifLinkWithinMixedContentStripsOnlyURL(t *testing.T) {
	var buf bytes.Buffer
	logger := newTestLogger(&buf)
	session := &discordgo.Session{State: discordgo.NewState()}
	m := &discordgo.Message{
		ID: "msg1", ChannelID: "chan1", Content: "check this out https://tenor.com/view/funny-123",
		Author: &discordgo.User{ID: "u1", Username: "alice"},
	}

	mappings := newFakeMappingReader()
	mappings.set(string(engine.EntityChannel), "chan1", engine.Mapping{Found: true, StoatID: "stoat-chan1", Status: engine.StatusActive})

	writer := &fakeMessageWriter{sendReturns: "stoat-msg1"}
	op, ok := BuildMessageOp(engine.OpCreate, session, "guild1", m, mappings, writer, logger)
	if !ok {
		t.Fatal("expected ok=true")
	}

	if _, err := op.Apply(context.Background()); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if writer.sendMsg.Content != "check this out" {
		t.Fatalf("sendMsg.Content = %q, want %q", writer.sendMsg.Content, "check this out")
	}
	if len(writer.sendMsg.Attachments) != 1 {
		t.Fatalf("sendMsg.Attachments = %v, want 1 entry", writer.sendMsg.Attachments)
	}
}

func TestBuildMessageOp_NonGifLinkContentUnaffected(t *testing.T) {
	var buf bytes.Buffer
	logger := newTestLogger(&buf)
	session := &discordgo.Session{State: discordgo.NewState()}
	m := &discordgo.Message{
		ID: "msg1", ChannelID: "chan1", Content: "https://example.com/cat.gif",
		Author: &discordgo.User{ID: "u1", Username: "alice"},
	}

	mappings := newFakeMappingReader()
	mappings.set(string(engine.EntityChannel), "chan1", engine.Mapping{Found: true, StoatID: "stoat-chan1", Status: engine.StatusActive})

	writer := &fakeMessageWriter{sendReturns: "stoat-msg1"}
	op, ok := BuildMessageOp(engine.OpCreate, session, "guild1", m, mappings, writer, logger)
	if !ok {
		t.Fatal("expected ok=true")
	}

	if _, err := op.Apply(context.Background()); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if writer.sendMsg.Content != "https://example.com/cat.gif" {
		t.Fatalf("sendMsg.Content = %q, want unchanged", writer.sendMsg.Content)
	}
	if len(writer.sendMsg.Attachments) != 0 {
		t.Fatalf("sendMsg.Attachments = %v, want none (unlisted host is not detected)", writer.sendMsg.Attachments)
	}
}

func TestBuildMessageOp_EditWithGifLinkLeavesContentUntouched(t *testing.T) {
	var buf bytes.Buffer
	logger := newTestLogger(&buf)
	session := &discordgo.Session{State: discordgo.NewState()}
	m := &discordgo.Message{
		ID: "msg1", ChannelID: "chan1", Content: "https://tenor.com/view/funny-123",
		Author: &discordgo.User{ID: "u1", Username: "alice"},
	}

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

	if writer.editContent != "https://tenor.com/view/funny-123" {
		t.Fatalf("editContent = %q, want the link left intact (edit cannot attach)", writer.editContent)
	}
	if len(writer.uploadURLs) != 0 {
		t.Fatalf("uploadURLs = %v, want none (edit path never uploads)", writer.uploadURLs)
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
