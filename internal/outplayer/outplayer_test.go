package outplayer

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNormalizeDir(t *testing.T) {
	cases := map[string]string{
		"":            "/",
		"/":           "/",
		"Inbox":       "/Inbox/",
		"/Inbox":      "/Inbox/",
		"/Inbox/":     "/Inbox/",
		"  Movies  ":  "/Movies/",
		"a/b":         "/a/b/",
		"///nested//": "/nested/",
	}
	for in, want := range cases {
		if got := NormalizeDir(in); got != want {
			t.Errorf("NormalizeDir(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFormatBytes(t *testing.T) {
	cases := map[int64]string{
		0:          "0 B",
		512:        "512 B",
		1024:       "1.0 KB",
		1536:       "1.5 KB",
		1048576:    "1.0 MB",
		7755082578: "7.2 GB",
	}
	for in, want := range cases {
		if got := formatBytes(in); got != want {
			t.Errorf("formatBytes(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestExtractError(t *testing.T) {
	html := `<!DOCTYPE html><html><head><title>HTTP Error 500</title></head><body>` +
		`<h1>HTTP Error 500: Failed moving uploaded file to &quot;/Inbox/&quot;</h1>` +
		`<h3>details</h3></body></html>`
	got := extractError(html)
	want := `HTTP Error 500: Failed moving uploaded file to "/Inbox/"`
	if got != want {
		t.Errorf("extractError = %q, want %q", got, want)
	}

	if got := extractError("plain text error"); got != "plain text error" {
		t.Errorf("extractError(plain) = %q", got)
	}
	if got := extractError(""); got != "no response body" {
		t.Errorf("extractError(empty) = %q", got)
	}
}

func TestReachable(t *testing.T) {
	// A server that answers /list with 200 is considered reachable.
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/list" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("[]"))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ok.Close()

	if err := Reachable(context.Background(), ok.URL); err != nil {
		t.Errorf("Reachable(ok) returned error: %v", err)
	}

	// A non-200 response is not reachable.
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer bad.Close()
	if err := Reachable(context.Background(), bad.URL); err == nil {
		t.Error("Reachable(bad) expected error, got nil")
	}

	// An unroutable URL fails to connect.
	if err := Reachable(context.Background(), "http://127.0.0.1:1"); err == nil {
		t.Error("Reachable(unreachable) expected error, got nil")
	}
}

func TestPostFile(t *testing.T) {
	var gotPath, gotName, gotContent string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/upload" || r.Method != http.MethodPost {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		gotPath = r.FormValue("path")
		f, hdr, err := r.FormFile("files[]")
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		defer f.Close()
		gotName = hdr.Filename
		b, _ := io.ReadAll(f)
		gotContent = string(b)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	content := "the quick brown fox"
	status, _, err := postFile(context.Background(), srv.URL+"/upload", "/", "movie.mkv", strings.NewReader(content))
	if err != nil {
		t.Fatalf("postFile returned error: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("postFile status = %d, want 200", status)
	}
	if gotPath != "/" {
		t.Errorf("path field = %q, want %q", gotPath, "/")
	}
	if gotName != "movie.mkv" {
		t.Errorf("filename = %q, want %q", gotName, "movie.mkv")
	}
	if gotContent != content {
		t.Errorf("uploaded content = %q, want %q", gotContent, content)
	}
}

func TestPostFileReportsServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`<html><body><h1>HTTP Error 500: nope</h1></body></html>`))
	}))
	defer srv.Close()

	status, body, err := postFile(context.Background(), srv.URL+"/upload", "/", "x.mkv", strings.NewReader("data"))
	if err != nil {
		t.Fatalf("postFile returned transport error: %v", err)
	}
	if status != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", status)
	}
	if msg := extractError(body); msg != "HTTP Error 500: nope" {
		t.Errorf("extractError = %q", msg)
	}
}
