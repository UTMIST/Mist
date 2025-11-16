package cmd

import (
	"encoding/json"
	"net/http"
	"os"
	"time"

	"github.com/alecthomas/kong"
)

type Config struct {
	AccessToken string `json:"access_token"`
	// maybe APIBaseURL, etc.
}

type AppContext struct {
	Config     *Config
	HTTPClient *http.Client
}

type Globals struct {
	ConfigPath string `name:"config" help:"Path to config file" default:"${config_path}"`
}

type CLI struct {
	Globals

	// Define your CLI structure here: Top Level Commands
	Auth AuthCmd `cmd:"" help:"Authentication commands"`
	Job  JobCmd  `cmd:"" help:"Job management commands"`
	// Config ConfigCmd `cmd:"" help:"Configuration commands"`
	Help HelpCmd `cmd:"" help:"Show help information"`
	// Config ConfigCmd `cmd:"" help: "Display Cluster Configuration"`
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
	}

	err := kctx.Run()
	kctx.FatalIfErrorf(err)
	// fmt.Println("Command executed successfully")
}
