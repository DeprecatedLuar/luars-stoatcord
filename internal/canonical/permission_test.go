package canonical

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func newTestLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

func TestStoatBits_SingleKnownPermission(t *testing.T) {
	var buf bytes.Buffer
	logger := newTestLogger(&buf)

	got := StoatBits([]Permission{PermViewChannel}, logger)

	const want = uint64(1) << 20
	if got != want {
		t.Errorf("StoatBits(VIEW_CHANNEL) = %d, want %d", got, want)
	}
	if buf.Len() != 0 {
		t.Errorf("expected no log output for a known permission, got: %s", buf.String())
	}
}

func TestStoatBits_AggregatesMultiplePermissions(t *testing.T) {
	var buf bytes.Buffer
	logger := newTestLogger(&buf)

	got := StoatBits([]Permission{PermViewChannel, PermSendMessages}, logger)

	want := (uint64(1) << 20) | (uint64(1) << 22)
	if got != want {
		t.Errorf("StoatBits(VIEW_CHANNEL, SEND_MESSAGES) = %d, want %d", got, want)
	}
}

func TestStoatBits_EmptyInputReturnsZero(t *testing.T) {
	var buf bytes.Buffer
	logger := newTestLogger(&buf)

	got := StoatBits(nil, logger)

	if got != 0 {
		t.Errorf("StoatBits(nil) = %d, want 0", got)
	}
	if buf.Len() != 0 {
		t.Errorf("expected no log output for empty input, got: %s", buf.String())
	}
}

func TestStoatBits_DropsUnmappedPermissionAndLogs(t *testing.T) {
	var buf bytes.Buffer
	logger := newTestLogger(&buf)

	got := StoatBits([]Permission{PermUseAppCommands}, logger)

	if got != 0 {
		t.Errorf("StoatBits(USE_APP_COMMANDS) = %d, want 0 (no Stoat equivalent)", got)
	}
	logged := buf.String()
	if !strings.Contains(logged, "USE_APP_COMMANDS") {
		t.Errorf("expected drop log to mention USE_APP_COMMANDS, got: %s", logged)
	}
	if !strings.Contains(strings.ToUpper(logged), "WARN") {
		t.Errorf("expected drop log at WARN level, got: %s", logged)
	}
}

func TestStoatBits_DropDoesNotBlockOtherPermissions(t *testing.T) {
	var buf bytes.Buffer
	logger := newTestLogger(&buf)

	got := StoatBits([]Permission{PermViewChannel, PermSendTTS}, logger)

	want := uint64(1) << 20
	if got != want {
		t.Errorf("StoatBits(VIEW_CHANNEL, SEND_TTS) = %d, want %d (drop should not affect other bits)", got, want)
	}
	if !strings.Contains(buf.String(), "SEND_TTS") {
		t.Errorf("expected drop log for SEND_TTS, got: %s", buf.String())
	}
}

func TestOverwrite_ToStoat_TranslatesAllowAndDenySeparately(t *testing.T) {
	var buf bytes.Buffer
	logger := newTestLogger(&buf)

	ow := Overwrite{
		Allow: []Permission{PermViewChannel, PermSendMessages},
		Deny:  []Permission{PermAddReactions},
	}

	got := ow.ToStoat(logger)

	wantAllow := (uint64(1) << 20) | (uint64(1) << 22)
	wantDeny := uint64(1) << 29
	if got.Allow != wantAllow {
		t.Errorf("ToStoat().Allow = %d, want %d", got.Allow, wantAllow)
	}
	if got.Deny != wantDeny {
		t.Errorf("ToStoat().Deny = %d, want %d", got.Deny, wantDeny)
	}
}

func TestOverwrite_ToStoat_EmptyOverwriteIsZero(t *testing.T) {
	var buf bytes.Buffer
	logger := newTestLogger(&buf)

	got := Overwrite{}.ToStoat(logger)

	if got.Allow != 0 || got.Deny != 0 {
		t.Errorf("ToStoat() of empty overwrite = %+v, want zero value", got)
	}
}
