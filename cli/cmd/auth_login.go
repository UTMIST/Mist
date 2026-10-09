package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"golang.org/x/term"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

type LoginCmd struct {
	Email         string `help:"Mist member email" required:""`
	PasswordStdin bool   `help:"Read password from standard input (for automation)"`
}

func (l *LoginCmd) Run(ctx *AppContext) error {
	var password string
	if l.PasswordStdin {
		data, err := io.ReadAll(io.LimitReader(os.Stdin, 130))
		if err != nil {
			return err
		}
		password = strings.TrimSpace(string(data))
	} else {
		fmt.Print("Password: ")
		data, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println()
		if err != nil {
			return fmt.Errorf("password input: %w (use --password-stdin for automation)", err)
		}
		password = string(data)
	}
	base := ctx.baseURL()
	u, err := url.Parse(base)
	if err != nil {
		return err
	}
	u.Path = "/auth/sign-in/email"
	u.RawQuery = ""
	body, _ := json.Marshal(map[string]string{"email": l.Email, "password": password})
	req, err := http.NewRequest(http.MethodPost, u.String(), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", u.Scheme+"://"+u.Host)
	response, err := ctx.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return fmt.Errorf("login failed (HTTP %d)", response.StatusCode)
	}
	cookies := []string{}
	for _, cookie := range response.Cookies() {
		if strings.Contains(cookie.Name, "session_token") && cookie.Value != "" {
			cookies = append(cookies, cookie.Name+"="+cookie.Value)
		}
	}
	if len(cookies) == 0 {
		return fmt.Errorf("login returned no session cookie")
	}
	if ctx.Config == nil {
		ctx.Config = &Config{}
	}
	ctx.Config.SessionCookie = strings.Join(cookies, "; ")
	ctx.Config.AccessToken = ""
	ctx.Config.APIBaseURL = base
	if err := ctx.saveConfig(); err != nil {
		return err
	}
	fmt.Println("Signed in. Session saved in protected CLI configuration.")
	return nil
}
func (ctx *AppContext) saveConfig() error {
	path := ctx.ConfigPath
	if path == "" {
		path = defaultConfigPath()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(ctx.Config, "", "  ")
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".mist-config-")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if err = file.Chmod(0600); err != nil {
		file.Close()
		return err
	}
	if _, err = file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}

type LogoutCmd struct{}

func (l *LogoutCmd) Run(ctx *AppContext) error {
	if ctx.Config == nil || ctx.Config.SessionCookie == "" {
		fmt.Println("Not signed in.")
		return nil
	}
	base, err := url.Parse(ctx.baseURL())
	if err != nil {
		return err
	}
	origin := base.Scheme + "://" + base.Host
	base.Path = "/auth/sign-out"
	base.RawQuery = ""
	req, err := http.NewRequest(http.MethodPost, base.String(), strings.NewReader("{}"))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", origin)
	req.Header.Set("Cookie", ctx.Config.SessionCookie)
	response, err := ctx.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	response.Body.Close()
	if response.StatusCode != 200 {
		return fmt.Errorf("sign out failed (HTTP %d)", response.StatusCode)
	}
	ctx.Config.SessionCookie = ""
	ctx.Config.AccessToken = ""
	if err := ctx.saveConfig(); err != nil {
		return err
	}
	fmt.Println("Signed out.")
	return nil
}
