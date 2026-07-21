package stoat

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newTestClient wires a Client whose Settings.Features.Autumn.URL points at
// autumnSrv, the same way New() resolves it from a real instance's GET /.
func newTestClient(t *testing.T, autumnSrv *httptest.Server) *Client {
	t.Helper()
	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"revolt":"0.14.2","ws":"wss://events.stoat.chat","app":"stoat.chat","features":{"autumn":{"enabled":true,"url":%q}}}`, autumnSrv.URL)
	}))
	t.Cleanup(apiSrv.Close)

	client, err := New("test-token", apiSrv.URL)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return client
}

func TestUploadFromURL_DownloadsAndUploadsToAutumn(t *testing.T) {
	const fileBody = "fake image bytes"
	cdnSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/attachments/abc123/photo.png" {
			t.Errorf("cdn request path = %q, want /attachments/abc123/photo.png", r.URL.Path)
		}
		w.Write([]byte(fileBody))
	}))
	defer cdnSrv.Close()

	var gotTagPath string
	var gotFilename string
	var gotBody string
	autumnSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotTagPath = r.URL.Path
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatalf("ParseMultipartForm: %v", err)
		}
		file, header, err := r.FormFile("file")
		if err != nil {
			t.Fatalf("FormFile: %v", err)
		}
		defer file.Close()
		gotFilename = header.Filename
		buf := make([]byte, len(fileBody))
		n, _ := file.Read(buf)
		gotBody = string(buf[:n])

		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"autumn-id-1"}`))
	}))
	defer autumnSrv.Close()

	client := newTestClient(t, autumnSrv)

	id, err := client.UploadFromURL(context.Background(), "attachments", cdnSrv.URL+"/attachments/abc123/photo.png?ex=1234&sig=abcd")
	if err != nil {
		t.Fatalf("UploadFromURL: %v", err)
	}
	if id != "autumn-id-1" {
		t.Errorf("id = %q, want %q", id, "autumn-id-1")
	}
	if gotTagPath != "/attachments" {
		t.Errorf("autumn request path = %q, want /attachments", gotTagPath)
	}
	if gotFilename != "photo.png" {
		t.Errorf("uploaded filename = %q, want %q (query string must be stripped)", gotFilename, "photo.png")
	}
	if gotBody != fileBody {
		t.Errorf("uploaded body = %q, want %q", gotBody, fileBody)
	}
}

func TestUploadFromURL_CDNNonOKStatus(t *testing.T) {
	cdnSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer cdnSrv.Close()

	autumnSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("autumn server should not be contacted when the CDN download fails")
	}))
	defer autumnSrv.Close()

	client := newTestClient(t, autumnSrv)

	_, err := client.UploadFromURL(context.Background(), "attachments", cdnSrv.URL+"/gone.png")
	if err == nil {
		t.Fatal("UploadFromURL: want error on 404 download, got nil")
	}
}

func TestDownloadLimited_ExceedsLimitErrors(t *testing.T) {
	cdnSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(strings.Repeat("x", 10)))
	}))
	defer cdnSrv.Close()

	_, err := downloadLimited(context.Background(), http.DefaultClient, cdnSrv.URL, 5)
	if err == nil {
		t.Fatal("downloadLimited: want error when body exceeds limit, got nil")
	}
}

func TestDownloadLimited_WithinLimitSucceeds(t *testing.T) {
	const body = "hello"
	cdnSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(body))
	}))
	defer cdnSrv.Close()

	data, err := downloadLimited(context.Background(), http.DefaultClient, cdnSrv.URL, int64(len(body)))
	if err != nil {
		t.Fatalf("downloadLimited: %v", err)
	}
	if string(data) != body {
		t.Errorf("data = %q, want %q", string(data), body)
	}
}

func TestFilenameFromURL_StripsQueryString(t *testing.T) {
	got := filenameFromURL("https://cdn.discordapp.com/attachments/1/2/photo.png?ex=1&is=2&hm=3")
	if got != "photo.png" {
		t.Errorf("filenameFromURL = %q, want %q", got, "photo.png")
	}
}
