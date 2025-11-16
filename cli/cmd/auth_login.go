package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// TODO: Update with real auth URL
const authUrl = "https://example.com/login"

type LoginCmd struct {
}

func openUrl() error {

	var cmd *exec.Cmd
	url := authUrl
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default: // Linux, BSD, etc.
		cmd = exec.Command("xdg-open", url)
	}

	return cmd.Start()
}

func saveTokenToConfig(ctx *AppContext, token string) error {
	configPath := defaultConfigPath()
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}
	cfg := &Config{
		AccessToken: token,
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}
	if err := os.WriteFile(configPath, data, 0o600); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}
	fmt.Println("Token saved:", token)
	return nil
}

func getLongLivedToken(shortLivedToken string) (string, error) {
	// Placeholder for actual implementation to exchange short-lived token for long-lived token
	// In a real scenario, this would involve making an HTTP request to the auth server
	return shortLivedToken + "_long_lived", nil
}

func (l *LoginCmd) Run(ctx *AppContext) error {
	// mist auth login
	if ctx.Config != nil && ctx.Config.AccessToken != "" {

		// Already logged in, ask if they want to re-login
		fmt.Println("Already logged in with token:", ctx.Config.AccessToken)
		fmt.Print("Re-enter token? (y/N): ")
		reader := bufio.NewReader(os.Stdin)
		answer, _ := reader.ReadString('\n')
		answer = strings.TrimSpace(strings.ToLower(answer))
		if answer != "y" && answer != "yes" {
			fmt.Println("Aborting login.")
			return nil
		}
	}

	fmt.Println("Opening browser for authentication...")
	fmt.Printf("If your browser didn't open, click here: \033]8;;%s\033\\%s\033]8;;\033\\\n", authUrl, authUrl)

	err := openUrl()
	if err != nil {
		fmt.Println("Error opening browser:", err)
		return err
	}
	fmt.Print("token: ")

	reader := bufio.NewReader(os.Stdin)
	token, _ := reader.ReadString('\n')
	token = strings.TrimSpace(strings.ToLower(token))

	token, err = getLongLivedToken(token)
	if err != nil {
		fmt.Println("Error obtaining long-lived token:", err)
		return err
	}

	err = saveTokenToConfig(ctx, token)
	if err != nil {
		fmt.Println("Error during token saving")
		return err
	}

	fmt.Println("Saved token to config")

	return nil
}
