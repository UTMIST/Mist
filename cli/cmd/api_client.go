package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func (ctx *AppContext) api(method, path string, body, out interface{}) error {
	if ctx == nil {
		return fmt.Errorf("missing application context")
	}
	base := ctx.baseURL()
	var data []byte
	var err error
	if body != nil {
		data, err = json.Marshal(body)
		if err != nil {
			return err
		}
	}
	req, err := http.NewRequest(method, strings.TrimRight(base, "/")+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if team := ctx.teamID(); team != "" {
		req.Header.Set("X-Mist-Team", team)
	}
	if ctx.Config != nil && ctx.Config.SessionCookie != "" {
		if err := ctx.attachSession(req); err != nil {
			return err
		}
	}
	response, err := ctx.credentialClient().Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(response.Body, 8192))
		var errorBody struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(data, &errorBody) == nil && errorBody.Error != "" {
			return fmt.Errorf("API %d: %s", response.StatusCode, errorBody.Error)
		}
		return fmt.Errorf("API %d: %s", response.StatusCode, strings.TrimSpace(string(data)))
	}
	if out != nil {
		return json.NewDecoder(response.Body).Decode(out)
	}
	return nil
}

// Credentials belong to one exact scheme, host and port. An API path is not
// part of the origin; changing environments requires a separate --config/login.
func credentialOrigin(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return "", fmt.Errorf("invalid compute URL")
	}
	ip := net.ParseIP(u.Hostname())
	if u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "localhost" || (ip != nil && ip.IsLoopback()))) {
		return "", fmt.Errorf("credentials require HTTPS (HTTP only on loopback)")
	}
	return u.Scheme + "://" + strings.ToLower(u.Host), nil
}

func (ctx *AppContext) attachSession(req *http.Request) error {
	origin, err := credentialOrigin(req.URL.String())
	if err != nil {
		return err
	}
	saved, err := credentialOrigin(ctx.Config.APIBaseURL)
	if err != nil || origin != saved {
		return fmt.Errorf("session belongs to a different API origin; use a separate --config and sign in there")
	}
	req.Header.Set("Cookie", ctx.Config.SessionCookie)
	return nil
}

func (ctx *AppContext) credentialClient() *http.Client {
	client := http.Client{Timeout: 30 * time.Second}
	if ctx.HTTPClient != nil {
		client = *ctx.HTTPClient
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	client.Jar = nil // Only explicitly bound cookies may accompany these requests.
	return &client
}

func (ctx *AppContext) baseURL() string {
	if ctx.APIBaseURL != "" {
		return strings.TrimRight(ctx.APIBaseURL, "/")
	}
	if ctx.Config != nil && ctx.Config.APIBaseURL != "" {
		return strings.TrimRight(ctx.Config.APIBaseURL, "/")
	}
	return "http://127.0.0.1:3000"
}

func (ctx *AppContext) teamID() string {
	if ctx.TeamID == "legacy" {
		return ""
	}
	if ctx.TeamID != "" {
		return ctx.TeamID
	}
	if ctx.Config != nil {
		return ctx.Config.TeamID
	}
	return ""
}
