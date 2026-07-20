package discord

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"
	"github.com/luar/stoatcord/internal/canonical"
)

func newTestLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

func TestPermissionsFromBits_SingleKnownPermission(t *testing.T) {
	var buf bytes.Buffer
	logger := newTestLogger(&buf)

	got := PermissionsFromBits(discordgo.PermissionViewChannel, logger)

	if len(got) != 1 || got[0] != canonical.PermViewChannel {
		t.Errorf("PermissionsFromBits(ViewChannel) = %v, want [VIEW_CHANNEL]", got)
	}
	if buf.Len() != 0 {
		t.Errorf("expected no log output for a known permission, got: %s", buf.String())
	}
}

func TestPermissionsFromBits_DecodesMultipleSetBits(t *testing.T) {
	var buf bytes.Buffer
	logger := newTestLogger(&buf)

	bits := int64(discordgo.PermissionViewChannel | discordgo.PermissionSendMessages)
	got := PermissionsFromBits(bits, logger)

	want := map[canonical.Permission]bool{canonical.PermViewChannel: true, canonical.PermSendMessages: true}
	if len(got) != 2 {
		t.Fatalf("PermissionsFromBits() = %v, want 2 permissions", got)
	}
	for _, p := range got {
		if !want[p] {
			t.Errorf("unexpected permission %v in result %v", p, got)
		}
	}
}

func TestPermissionsFromBits_ZeroReturnsEmpty(t *testing.T) {
	var buf bytes.Buffer
	logger := newTestLogger(&buf)

	got := PermissionsFromBits(0, logger)

	if len(got) != 0 {
		t.Errorf("PermissionsFromBits(0) = %v, want empty", got)
	}
	if buf.Len() != 0 {
		t.Errorf("expected no log output for zero bits, got: %s", buf.String())
	}
}

func TestPermissionsFromBits_DropsAdministratorAndLogs(t *testing.T) {
	var buf bytes.Buffer
	logger := newTestLogger(&buf)

	got := PermissionsFromBits(discordgo.PermissionAdministrator, logger)

	if len(got) != 0 {
		t.Errorf("PermissionsFromBits(Administrator) = %v, want empty (no canonical equivalent)", got)
	}
	logged := buf.String()
	if !strings.Contains(logged, "ADMINISTRATOR") {
		t.Errorf("expected drop log to mention ADMINISTRATOR, got: %s", logged)
	}
	if !strings.Contains(strings.ToUpper(logged), "WARN") {
		t.Errorf("expected drop log at WARN level, got: %s", logged)
	}
}

func TestPermissionsFromBits_DropsUseSlashCommandsAndLogs(t *testing.T) {
	var buf bytes.Buffer
	logger := newTestLogger(&buf)

	got := PermissionsFromBits(discordgo.PermissionUseApplicationCommands, logger)

	if len(got) != 0 {
		t.Errorf("PermissionsFromBits(UseApplicationCommands) = %v, want empty", got)
	}
	if !strings.Contains(buf.String(), "USE_APP_COMMANDS") {
		t.Errorf("expected drop log to mention USE_APP_COMMANDS, got: %s", buf.String())
	}
}

func TestPermissionsFromBits_DropDoesNotBlockOtherBits(t *testing.T) {
	var buf bytes.Buffer
	logger := newTestLogger(&buf)

	bits := int64(discordgo.PermissionViewChannel | discordgo.PermissionAdministrator)
	got := PermissionsFromBits(bits, logger)

	if len(got) != 1 || got[0] != canonical.PermViewChannel {
		t.Errorf("PermissionsFromBits() = %v, want [VIEW_CHANNEL] (drop shouldn't block other bits)", got)
	}
}
