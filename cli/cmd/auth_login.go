package cmd

import (
	"bufio"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	serverBaseUrl = "http://localhost:3000"
	authUrl       = serverBaseUrl + "/oauth/authorize"
	tokenUrl      = serverBaseUrl + "/oauth/token"
	clientID      = "cli"
	redirectURI   = serverBaseUrl + "/oauth/callback"
)

type LoginCmd struct {
}

func generateRandomString(n int) (string, error) {
	b := make([]byte, n)
	_, err := rand.Read(b)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func generateCodeChallenge(verifier string) string {
	s := sha256.New()
	s.Write([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(s.Sum(nil))
}

func openUrl(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default: // Linux
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}

func saveTokenToConfig(ctx *AppContext, token string, refreshToken string, expiresAt time.Time) error {
	configPath := defaultConfigPath()
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	if ctx.Config == nil {
		ctx.Config = &Config{}
	}
	ctx.Config.AccessToken = token
	ctx.Config.RefreshToken = refreshToken
	ctx.Config.ExpiresAt = expiresAt

	data, err := json.MarshalIndent(ctx.Config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}
	if err := os.WriteFile(configPath, data, 0o600); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}
	fmt.Println("Token saved")
	return nil
}

func (ctx *AppContext) RefreshAccessToken() error {
	data := url.Values{}
	data.Set("grant_type", "refresh_token")
	data.Set("client_id", clientID)
	data.Set("refresh_token", ctx.Config.RefreshToken)

	resp, err := http.PostForm(tokenUrl, data)
	if err != nil {
		return fmt.Errorf("failed to refresh token: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("token refresh failed: %s", body)
	}

	var tokenResp map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return fmt.Errorf("failed to decode token response: %w", err)
	}

	accessToken, _ := tokenResp["access_token"].(string)
	refreshToken, _ := tokenResp["refresh_token"].(string)
	expiresIn, _ := tokenResp["expires_in"].(float64)
	expiresAt := time.Now().Add(time.Duration(expiresIn) * time.Second)

	return saveTokenToConfig(ctx, accessToken, refreshToken, expiresAt)
}

func (ctx *AppContext) CheckValidToken() error {
	if ctx.Config == nil || ctx.Config.AccessToken == "" {
		return fmt.Errorf("not logged in")
	}

	if time.Now().Add(30 * time.Second).After(ctx.Config.ExpiresAt) {
		return ctx.RefreshAccessToken()
	}

	return nil
}

func (l *LoginCmd) Run(ctx *AppContext) error {
	verifier, err := generateRandomString(32)
	if err != nil {
		return fmt.Errorf("failed to generate verifier: %w", err)
	}
	challenge := generateCodeChallenge(verifier)

	u, _ := url.Parse(authUrl)
	q := u.Query()
	q.Set("client_id", clientID)
	q.Set("response_type", "code")
	q.Set("redirect_uri", redirectURI)
	q.Set("code_challenge", challenge)
	q.Set("code_challenge_method", "S256")
	u.RawQuery = q.Encode()

	fmt.Printf("If your browser doesn't open, visit: %s\n", u.String())

	if err := openUrl(u.String()); err != nil {
		fmt.Printf("Failed to open browser: %v\n", err)
	}

	fmt.Print("Enter the authorization code: ")
	reader := bufio.NewReader(os.Stdin)
	code, _ := reader.ReadString('\n')
	code = strings.TrimSpace(code)

	if code == "" {
		return fmt.Errorf("authorization code is required")
	}

	data := url.Values{}
	data.Set("grant_type", "authorization_code")
	data.Set("client_id", clientID)
	data.Set("code", code)
	data.Set("redirect_uri", redirectURI)
	data.Set("code_verifier", verifier)

	resp, err := http.PostForm(tokenUrl, data)
	if err != nil {
		return fmt.Errorf("failed to exchange token: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("token exchange failed: %s", body)
	}

	var tokenResp map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return fmt.Errorf("failed to decode token response: %w", err)
	}

	accessToken, ok := tokenResp["access_token"].(string)
	if !ok {
		return fmt.Errorf("invalid token response: missing access_token")
	}

	refreshToken, _ := tokenResp["refresh_token"].(string)
	expiresIn, _ := tokenResp["expires_in"].(float64)

	expiresAt := time.Now().Add(time.Duration(expiresIn) * time.Second)

	return saveTokenToConfig(ctx, accessToken, refreshToken, expiresAt)
}
