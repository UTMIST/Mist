package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"golang.org/x/term"
)

func (l *LoginCmd) loginMisty(ctx *AppContext) error {
	base, err := url.Parse(ctx.baseURL())
	if err != nil || base.Host == "" || base.User != nil {
		return fmt.Errorf("invalid compute URL")
	}
	ip := net.ParseIP(base.Hostname())
	if base.Scheme != "https" && !(base.Scheme == "http" && (base.Hostname() == "localhost" || (ip != nil && ip.IsLoopback()))) {
		return fmt.Errorf("Misty login requires HTTPS (HTTP only on loopback)")
	}
	// Never follow a login redirect with a code or binding cookie.
	client := *ctx.HTTPClient
	client.Timeout = 15 * time.Second
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	origin := base.Scheme + "://" + base.Host
	post := func(path string, body any, cookies []*http.Cookie) (*http.Response, error) {
		data, _ := json.Marshal(body)
		req, err := http.NewRequest(http.MethodPost, origin+path, bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("invalid login request")
		}
		req.Header.Set("Origin", origin)
		req.Header.Set("Content-Type", "application/json")
		for _, cookie := range cookies {
			req.AddCookie(cookie)
		}
		response, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("compute login unavailable")
		}
		return response, nil
	}
	start, err := post("/auth/misty/start", map[string]string{"email": l.Email}, nil)
	if err != nil {
		return err
	}
	defer start.Body.Close()
	if start.StatusCode != 202 {
		return fmt.Errorf("requesting code failed (HTTP %d)", start.StatusCode)
	}
	var challenge struct {
		ID string `json:"challenge_id"`
	}
	if json.NewDecoder(io.LimitReader(start.Body, 8192)).Decode(&challenge) != nil || challenge.ID == "" {
		return fmt.Errorf("invalid login response")
	}
	var cookies []*http.Cookie
	for _, cookie := range start.Cookies() {
		if cookie.Name == "misty_login" {
			cookies = append(cookies, cookie)
		}
	}
	if len(cookies) != 1 {
		return fmt.Errorf("login returned no challenge cookie")
	}
	fmt.Fprintln(os.Stderr, "If your email belongs to an active Misty account, a code will arrive shortly.")
	var code []byte
	if l.CodeStdin {
		code, err = io.ReadAll(io.LimitReader(os.Stdin, 16))
	} else {
		fmt.Fprint(os.Stderr, "Verification code: ")
		code, err = term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
	}
	if err != nil {
		return fmt.Errorf("cannot read code (use --code-stdin for automation)")
	}
	value := strings.TrimSpace(string(code))
	if !regexp.MustCompile(`^[0-9]{6}$`).MatchString(value) {
		return fmt.Errorf("enter a six-digit code")
	}
	confirm, err := post("/auth/misty/confirm", map[string]string{"challenge_id": challenge.ID, "code": value}, cookies)
	if err != nil {
		return err
	}
	defer confirm.Body.Close()
	if confirm.StatusCode != 200 {
		return fmt.Errorf("code verification failed (HTTP %d); check the code or start again", confirm.StatusCode)
	}
	return ctx.saveLogin(ctx.baseURL(), confirm)
}
