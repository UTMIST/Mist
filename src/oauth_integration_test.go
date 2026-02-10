package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	log2 "mist/multilogger"
)

// Helper for PKCE
func generateCodeChallenge(verifier string) string {
	s := sha256.New()
	s.Write([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(s.Sum(nil))
}

func TestOAuthFlow(t *testing.T) {
	// Setup app
	cfg, err := log2.GetLogConfig()
	if err != nil {
		t.Fatalf("Failed to get log config: %v", err)
	}
	log, err := log2.CreateLogger("test", &cfg)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}

	app := NewApp("memory", "TestGPU", log)

	// Create test server
	ts := httptest.NewServer(app.httpServer.Handler)
	defer ts.Close()

	// 1. Authorize Request
	clientID := "demo-client-id"
	verifier := "some-random-secret-verifier-string-1234567890" // High entropy string
	challenge := generateCodeChallenge(verifier)
	redirectURI := "http://localhost:3000"

	authURL := fmt.Sprintf("%s/oauth/authorize?client_id=%s&response_type=code&redirect_uri=%s&code_challenge=%s&code_challenge_method=S256",
		ts.URL, clientID, url.QueryEscape(redirectURI), challenge)

	t.Logf("Requesting authorization: %s", authURL)

	client := ts.Client()
	client.Timeout = 5 * time.Second
	// Disable redirect following to inspect the location
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}

	resp, err := client.Get(authURL)
	if err != nil {
		t.Fatalf("Failed to GET authorize: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusFound {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("Expected 302 Found, got %d. Body: %s", resp.StatusCode, body)
	}

	loc, err := resp.Location()
	if err != nil {
		t.Fatalf("Failed to get location: %v", err)
	}

	code := loc.Query().Get("code")
	if code == "" {
		t.Fatalf("No code in redirect location: %s", loc.String())
	}
	t.Logf("Got auth code: %s", code)

	// 2. Token Exchange
	tokenURL := fmt.Sprintf("%s/oauth/token", ts.URL)
	data := url.Values{}
	data.Set("grant_type", "authorization_code")
	data.Set("client_id", clientID)
	data.Set("client_secret", "demo-client-secret")
	data.Set("code", code)
	data.Set("redirect_uri", redirectURI)
	data.Set("code_verifier", verifier)

	t.Logf("Exchanging token at: %s", tokenURL)

	resp, err = client.PostForm(tokenURL, data)
	if err != nil {
		t.Fatalf("Failed to POST token: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("Expected 200 OK, got %d. Body: %s", resp.StatusCode, body)
	}

	var tokenResp map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		t.Fatalf("Failed to decode token response: %v", err)
	}

	if _, ok := tokenResp["access_token"]; !ok {
		t.Errorf("No access_token in response: %v", tokenResp)
	}
	t.Logf("Token response: %v", tokenResp)
}
