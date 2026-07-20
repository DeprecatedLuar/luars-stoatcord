package stoat

import (
	"context"
	"net/http"
	"sync"
	"testing"

	"github.com/luar/stoatcord/internal/canonical"
)

func TestFetchServer_DecodesNameCategoriesChannelsAndRoles(t *testing.T) {
	client, _ := newTestServer(t, func(mux *http.ServeMux, _ *sync.Mutex, _ *[]recordedRequest) {
		mux.HandleFunc("/servers/srv1", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{
				"name": "My Server",
				"channels": ["c1", "c2"],
				"categories": [
					{"id": "cat1", "title": "General", "channels": ["c1"]},
					{"id": "cat2", "title": "Voice", "channels": ["c2"]}
				],
				"roles": {
					"role1": {"name": "Admin", "permissions": {"a": 1, "d": 0}},
					"role2": {"name": "Member", "permissions": {"a": 0, "d": 0}}
				}
			}`))
		})
	})

	info, err := client.FetchServer(context.Background(), "srv1")
	if err != nil {
		t.Fatalf("FetchServer: %v", err)
	}

	if info.Name != "My Server" {
		t.Fatalf("Name = %q, want %q", info.Name, "My Server")
	}
	if len(info.ChannelIDs) != 2 || info.ChannelIDs[0] != "c1" {
		t.Fatalf("ChannelIDs = %+v", info.ChannelIDs)
	}
	if len(info.Categories) != 2 || info.Categories[0].ID != "cat1" || info.Categories[0].Title != "General" || len(info.Categories[0].ChannelIDs) != 1 {
		t.Fatalf("Categories = %+v", info.Categories)
	}

	roleNames := map[string]string{}
	for _, r := range info.Roles {
		roleNames[r.ID] = r.Name
	}
	if roleNames["role1"] != "Admin" || roleNames["role2"] != "Member" {
		t.Fatalf("Roles = %+v", info.Roles)
	}
}

func TestFetchChannel_TextChannelWithNoVoiceField(t *testing.T) {
	client, _ := newTestServer(t, func(mux *http.ServeMux, _ *sync.Mutex, _ *[]recordedRequest) {
		mux.HandleFunc("/channels/chan1", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"_id": "chan1", "name": "general", "channel_type": "TextChannel"}`))
		})
	})

	info, err := client.FetchChannel(context.Background(), "chan1")
	if err != nil {
		t.Fatalf("FetchChannel: %v", err)
	}
	if info.ID != "chan1" || info.Name != "general" || info.Type != canonical.ChannelTypeText {
		t.Fatalf("info = %+v, want text channel chan1/general", info)
	}
}

// Ground truth (stoatchat/stoatchat model.rs): VoiceChannel was merged into
// TextChannel on the wire -- channel_type reads "TextChannel" for voice
// channels too, so kind is only distinguishable by the "voice" field's
// presence, never by channel_type.
func TestFetchChannel_VoiceChannelDiscriminatedByVoiceField(t *testing.T) {
	client, _ := newTestServer(t, func(mux *http.ServeMux, _ *sync.Mutex, _ *[]recordedRequest) {
		mux.HandleFunc("/channels/chan2", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"_id": "chan2", "name": "voice-lounge", "channel_type": "TextChannel", "voice": {"max_users": 10}}`))
		})
	})

	info, err := client.FetchChannel(context.Background(), "chan2")
	if err != nil {
		t.Fatalf("FetchChannel: %v", err)
	}
	if info.Type != canonical.ChannelTypeVoice {
		t.Fatalf("Type = %q, want voice (channel_type alone is not a reliable discriminator)", info.Type)
	}
}
