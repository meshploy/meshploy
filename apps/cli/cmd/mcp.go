package cmd

import (
	mcpsdk "github.com/mark3labs/mcp-go/server"
	"github.com/meshploy/apps/cli/internal/mcpserver"
	"github.com/spf13/cobra"
)

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Start the Meshploy MCP server (for AI tool integration)",
	Long: `Starts an MCP (Model Context Protocol) server over stdio.

Configure Claude Code by adding to .claude/settings.json:
  {
    "mcpServers": {
      "meshploy": {
        "command": "meshploy",
        "args": ["mcp"]
      }
    }
  }

Requires an active login: meshploy auth login --api-url <url>`,
	RunE: func(cmd *cobra.Command, args []string) error {
		c := apiClient()
		oid := orgID()
		s := mcpserver.New(c, oid)
		return mcpsdk.ServeStdio(s)
	},
}

func init() {
	rootCmd.AddCommand(mcpCmd)
}
