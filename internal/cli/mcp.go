package cli

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/SeKondBrainAILabs/kemory-cli/internal/bridge"
)

func newMCPCmd(g *globals) *cobra.Command {
	mcp := &cobra.Command{
		Use:   "mcp",
		Short: "MCP integration: wire MCP hosts and run the bridge they launch.",
		Args:  cobra.NoArgs,
	}
	mcp.AddCommand(newMCPInstallCmd(g), &cobra.Command{
		Use:   "serve",
		Short: "Run the stdio MCP bridge (launched by the MCP host, not by people).",
		Long: "Run the stdio MCP bridge. MCP hosts launch it as `kemory --env <env> mcp serve`.\n" +
			"It relays tools, prompts, resources and completions to Kemory with the\n" +
			"credentials of the selected env, or KEMORY_API_KEY when set.",
		Args: cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			return bridge.Serve(g.env, os.Stdin, os.Stdout)
		},
	})
	return mcp
}
