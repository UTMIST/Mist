package cmd

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestSessionRejectsChangedOrigin(t *testing.T) {
	for _, logout := range []bool{false, true} {
		t.Run(map[bool]string{false: "API", true: "logout"}[logout], func(t *testing.T) {
			called := false
			other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
			defer other.Close()
			ctx := &AppContext{HTTPClient: other.Client(), APIBaseURL: other.URL, ConfigPath: filepath.Join(t.TempDir(), "config.json"), Config: &Config{APIBaseURL: "https://production.example.test/api", SessionCookie: "session_token=synthetic"}}
			var err error
			if logout {
				err = (&LogoutCmd{}).Run(ctx)
			} else {
				err = ctx.api("GET", "/session", nil, nil)
			}
			if err == nil || called {
				t.Fatal("different origin received an authenticated request")
			}
			if ctx.Config.SessionCookie == "" {
				t.Fatal("rejected logout erased the original session")
			}
		})
	}
}

func TestSessionRejectsRedirects(t *testing.T) {
	for _, logout := range []bool{false, true} {
		t.Run(map[bool]string{false: "API", true: "logout"}[logout], func(t *testing.T) {
			called := false
			plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
			defer plain.Close()
			secure := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, plain.URL, http.StatusTemporaryRedirect)
			}))
			defer secure.Close()
			ctx := &AppContext{HTTPClient: secure.Client(), ConfigPath: filepath.Join(t.TempDir(), "config.json"), Config: &Config{APIBaseURL: secure.URL, SessionCookie: "session_token=synthetic"}}
			var err error
			if logout {
				err = (&LogoutCmd{}).Run(ctx)
			} else {
				err = ctx.api("GET", "/session", nil, nil)
			}
			if err == nil || called {
				t.Fatal("authenticated redirect was followed")
			}
		})
	}
}

func TestSessionAcceptsSavedOrigin(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if r.Header.Get("Cookie") != "session_token=synthetic" {
			t.Error("missing session")
		}
	}))
	defer server.Close()
	ctx := &AppContext{HTTPClient: server.Client(), APIBaseURL: server.URL + "/api", Config: &Config{APIBaseURL: server.URL, SessionCookie: "session_token=synthetic"}}
	if err := ctx.api("GET", "/session", nil, nil); err != nil || !called {
		t.Fatal("saved origin rejected", err)
	}
}

func TestCredentialOriginRejectsUnsafeURLs(t *testing.T) {
	for _, raw := range []string{"http://compute.example.com/api", "https://user:password@compute.example.com", "https://compute.example.com?token=x", "https://compute.example.com#token", ""} {
		if _, err := credentialOrigin(raw); err == nil {
			t.Errorf("unsafe origin accepted: %q", raw)
		}
	}
}
