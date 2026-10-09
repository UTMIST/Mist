package cmd

import (
	"encoding/json"
	"net/http"
	"os"
	"time"

	"github.com/alecthomas/kong"
)

type Config struct {
	AccessToken   string `json:"access_token,omitempty"`
	SessionCookie string `json:"session_cookie,omitempty"`
	APIBaseURL    string `json:"api_base_url,omitempty"`
	TeamID        string `json:"team_id,omitempty"`
}

type AppContext struct {
	Config     *Config
	HTTPClient *http.Client
	APIBaseURL string
	ConfigPath string
	TeamID     string
}

type Globals struct {
	Workspace  string `name:"team" env:"MIST_TEAM" help:"Team workspace ID, or legacy (overrides saved workspace)"`
	ConfigPath string `name:"config" help:"Path to config file" default:"${config_path}"`
	APIURL     string `name:"api-url" env:"MIST_API_URL" help:"Mist API URL (default http://127.0.0.1:3000)"`
}

type CLI struct {
	Globals

	Team TeamCmd `cmd:"" help:"List and select team workspaces"`
	Auth AuthCmd `cmd:"" help:"Authentication commands"`
	Job  JobCmd  `cmd:"" help:"Job management commands"`
	Help HelpCmd `cmd:"" help:"Show help information"`
}

func loadConfig(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg Config
	if err := json.Unmarshal(b, &cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func Main() {
	var cli CLI
	// Read command-line arguments

	appCtx := &AppContext{
		HTTPClient: &http.Client{Timeout: 10 * time.Second},
	}

	kctx := kong.Parse(&cli,
		kong.Name("mist"),
		kong.Description("MIST CLI - Manage your MIST jobs and configurations"),
		kong.UsageOnError(),
		kong.Vars{"config_path": defaultConfigPath()},
		kong.Bind(appCtx),
	)

	if cfg, err := loadConfig(cli.ConfigPath); err == nil {
		appCtx.Config = cfg
	} else if !os.IsNotExist(err) {
		// config file is present but broken
		kctx.FatalIfErrorf(err)
	}

	appCtx.APIBaseURL = cli.APIURL
	appCtx.TeamID = cli.Globals.Workspace
	appCtx.ConfigPath = cli.ConfigPath
	err := kctx.Run()
	kctx.FatalIfErrorf(err)
}
