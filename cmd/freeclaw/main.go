// PicoClaw - Ultra-lightweight personal AI agent
// Inspired by and based on nanobot: https://github.com/HKUDS/nanobot
// License: MIT
//
// Copyright (c) 2026 PicoClaw contributors

package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/sipeed/freeclaw/cmd/freeclaw/internal"
	"github.com/sipeed/freeclaw/cmd/freeclaw/internal/agent"
	"github.com/sipeed/freeclaw/cmd/freeclaw/internal/auth"
	"github.com/sipeed/freeclaw/cmd/freeclaw/internal/cron"
	"github.com/sipeed/freeclaw/cmd/freeclaw/internal/gateway"
	"github.com/sipeed/freeclaw/cmd/freeclaw/internal/migrate"
	"github.com/sipeed/freeclaw/cmd/freeclaw/internal/onboard"
	"github.com/sipeed/freeclaw/cmd/freeclaw/internal/skills"
	"github.com/sipeed/freeclaw/cmd/freeclaw/internal/status"
	"github.com/sipeed/freeclaw/cmd/freeclaw/internal/version"
)

func NewPicoclawCommand() *cobra.Command {
	short := fmt.Sprintf("%s %s - Personal AI Assistant v%s\n\n", internal.Logo, internal.GetAssistantName(), internal.GetVersion())

	cmd := &cobra.Command{
		Use:     "freeclaw",
		Short:   short,
		Example: "freeclaw version",
	}

	cmd.AddCommand(
		onboard.NewOnboardCommand(),
		agent.NewAgentCommand(),
		auth.NewAuthCommand(),
		gateway.NewGatewayCommand(),
		status.NewStatusCommand(),
		cron.NewCronCommand(),
		migrate.NewMigrateCommand(),
		skills.NewSkillsCommand(),
		version.NewVersionCommand(),
	)

	return cmd
}

const (
	colorBlue = "\033[1;38;2;62;93;185m"
	colorRed  = "\033[1;38;2;213;70;70m"
	banner    = "\r\n" +
		colorBlue + "███████╗██████╗ ███████╗███████╗" + colorRed + " ██████╗██╗      █████╗ ██╗    ██╗\n" +
		colorBlue + "██╔════╝██╔══██╗██╔════╝██╔════╝" + colorRed + "██╔════╝██║     ██╔══██╗██║    ██║\n" +
		colorBlue + "█████╗  ██████╔╝█████╗  █████╗  " + colorRed + "██║     ██║     ███████║██║ █╗ ██║\n" +
		colorBlue + "██╔══╝  ██╔══██╗██╔══╝  ██╔══╝  " + colorRed + "██║     ██║     ██╔══██║██║███╗██║\n" +
		colorBlue + "██║     ██║  ██║███████╗███████╗" + colorRed + "╚██████╗███████╗██║  ██║╚███╔███╔╝\n" +
		colorBlue + "╚═╝     ╚═╝  ╚═╝╚══════╝╚══════╝" + colorRed + " ╚═════╝╚══════╝╚═╝  ╚═╝ ╚══╝╚══╝\n " +
		"\033[0m\r\n"
)

func main() {
	fmt.Printf("%s", banner)
	cmd := NewPicoclawCommand()
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}

