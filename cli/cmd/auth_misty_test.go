package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMistyCLIUsesBoundCodeAndSavesProtectedSession(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Origin") == "" || r.Header.Get("X-API-Key") != "" {
			t.Error("incorrect client authentication")
		}
		switch r.URL.Path {
		case "/auth/misty/start":
			http.SetCookie(w, &http.Cookie{Name: "misty_login", Value: "binding", Path: "/auth"})
			w.WriteHeader(202)
			json.NewEncoder(w).Encode(map[string]string{"challenge_id": "challenge"})
		case "/auth/misty/confirm":
			cookie, err := r.Cookie("misty_login")
			if err != nil || cookie.Value != "binding" {
				t.Error("binding missing")
			}
			var body map[string]string
			json.NewDecoder(r.Body).Decode(&body)
			if body["code"] != "123456" || body["challenge_id"] != "challenge" {
				t.Error("incorrect confirmation")
			}
			http.SetCookie(w, &http.Cookie{Name: "session_token", Value: "synthetic-session", HttpOnly: true})
			w.WriteHeader(200)
		default:
			t.Error("unexpected path", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	input, err := os.CreateTemp(t.TempDir(), "code")
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	input.WriteString("123456\n")
	input.Seek(0, 0)
	original := os.Stdin
	os.Stdin = input
	defer func() { os.Stdin = original }()
	ctx := &AppContext{HTTPClient: server.Client(), APIBaseURL: server.URL, ConfigPath: filepath.Join(t.TempDir(), "config.json")}
	if err := (&LoginCmd{Email: "member@example.com", Misty: true, CodeStdin: true}).Run(ctx); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(ctx.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "synthetic-session") || strings.Contains(string(data), "123456") || strings.Contains(string(data), "binding") {
		t.Fatal("incorrect persisted credentials")
	}
	info, err := os.Stat(ctx.ConfigPath)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("unprotected config", err)
	}
}
