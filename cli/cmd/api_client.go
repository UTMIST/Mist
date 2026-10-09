package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

func (ctx *AppContext) api(method, path string, body, out interface{}) error {
	if ctx == nil {
		return fmt.Errorf("missing application context")
	}
	base := ctx.APIBaseURL
	if base == "" && ctx.Config != nil {
		base = ctx.Config.APIBaseURL
	}
	if base == "" {
		base = "http://127.0.0.1:3000"
	}
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
	if ctx.Config != nil && ctx.Config.AccessToken != "" {
		req.Header.Set("Authorization", "Bearer "+ctx.Config.AccessToken)
	}
	client := ctx.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	response, err := client.Do(req)
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
