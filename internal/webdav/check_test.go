package webdav

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newWebDAVStub returns a server that answers PROPFIND with 207 when the
// expected Basic Auth credentials are supplied (or none are required).
func newWebDAVStub(t *testing.T, wantUser, wantPass string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PROPFIND" {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if wantUser != "" {
			user, pass, ok := r.BasicAuth()
			if !ok || user != wantUser || pass != wantPass {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
		}
		w.WriteHeader(http.StatusMultiStatus)
	}))
}

func TestCheck(t *testing.T) {
	srv := newWebDAVStub(t, "josh", "secret")
	defer srv.Close()

	ctx := context.Background()

	if err := Check(ctx, srv.URL, "josh", "secret"); err != nil {
		t.Errorf("Check(valid creds) = %v, want nil", err)
	}

	err := Check(ctx, srv.URL, "josh", "wrong")
	if err == nil || !strings.Contains(err.Error(), "authentication failed") {
		t.Errorf("Check(bad creds) = %v, want authentication error", err)
	}

	anon := newWebDAVStub(t, "", "")
	defer anon.Close()
	if err := Check(ctx, anon.URL, "", ""); err != nil {
		t.Errorf("Check(anonymous) = %v, want nil", err)
	}

	if err := Check(ctx, "", "", ""); err == nil {
		t.Error("Check(empty URL) = nil, want error")
	}

	if err := Check(ctx, "http://127.0.0.1:1", "", ""); err == nil {
		t.Error("Check(unreachable) = nil, want connection error")
	}
}
