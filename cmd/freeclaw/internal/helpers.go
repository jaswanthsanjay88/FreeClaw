package internal

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/sipeed/freeclaw/pkg/config"
)

const Logo = "🦞"

var (
	version   = "dev"
	gitCommit string
	buildTime string
	goVersion string
)

// GetPicoclawHome returns the freeclaw home directory.
// Priority: $FREECLAW_HOME > ~/.freeclaw
func GetPicoclawHome() string {
	if home := os.Getenv("FREECLAW_HOME"); home != "" {
		return home
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".freeclaw")
}

func GetConfigPath() string {
	if configPath := os.Getenv("FREECLAW_CONFIG"); configPath != "" {
		return configPath
	}
	return filepath.Join(GetPicoclawHome(), "config.json")
}

func LoadConfig() (*config.Config, error) {
	return config.LoadConfig(GetConfigPath())
}

// FormatVersion returns the version string with optional git commit
func FormatVersion() string {
	v := version
	if gitCommit != "" {
		v += fmt.Sprintf(" (git: %s)", gitCommit)
	}
	return v
}

// FormatBuildInfo returns build time and go version info
func FormatBuildInfo() (string, string) {
	build := buildTime
	goVer := goVersion
	if goVer == "" {
		goVer = runtime.Version()
	}
	return build, goVer
}

// GetVersion returns the version string
func GetVersion() string {
	return version
}

// GetAssistantName returns configured assistant display name.
// Falls back to Freeclaw if config is missing or invalid.
func GetAssistantName() string {
	cfg, err := LoadConfig()
	if err != nil || cfg == nil {
		return "Freeclaw"
	}
	return cfg.GetAssistantName()
}
