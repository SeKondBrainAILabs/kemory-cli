// Package cli defines the kemory commands.
package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/SeKondBrainAILabs/kemory-cli/internal/api"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/config"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/exits"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/render"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/ui"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/version"
)

// globals are the root flags, resolved before any subcommand runs.
type globals struct {
	envFlag string
	noColor bool
	output  string
	quiet   bool

	env     string
	profile config.Profile
	// screen is set while connect draws its guided screen; nil means plain lines.
	screen *screen
}

func newRoot() *cobra.Command {
	g := &globals{}
	root := &cobra.Command{
		Use:           "kemory",
		Short:         "Kemory — persistent memory for your AI agents.",
		Version:       version.Version,
		SilenceErrors: true,
		SilenceUsage:  true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			output := g.output
			if output == "" {
				output = os.Getenv("KEMORY_OUTPUT")
			}
			if output == "" {
				output = "text"
			}
			if output != "text" && output != "json" {
				return exits.New(fmt.Sprintf("invalid --output %q (choose: text, json)", output), exits.Usage)
			}
			render.Configure(g.noColor, output == "json", g.quiet)
			env, err := config.ResolveEnv(g.envFlag)
			if err != nil {
				return exits.New(err.Error(), exits.Failure)
			}
			g.env = env
			g.profile = config.EnvProfile(env)
			api.Command = strings.TrimPrefix(cmd.CommandPath(), cmd.Root().Name()+" ")
			api.Notice = render.Accent
			if render.Styled() && headed(cmd) {
				render.Echo("")
				render.Echo(" " + ui.Mark() + " " + ui.Bold(cmd.CommandPath()))
				render.Frame()
			}
			return nil
		},
	}
	root.SetVersionTemplate("kemory, version {{.Version}}\n")
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return exits.New(err.Error(), exits.Usage)
	})
	flags := root.PersistentFlags()
	flags.StringVar(&g.envFlag, "env", "", "Target environment: 'prod' (default), 'india' or 'local'. Overrides KEMORY_ENV. Each env keeps its own login.")
	flags.BoolVar(&g.noColor, "no-color", false, "Disable colour. The NO_COLOR environment variable does the same.")
	flags.StringVar(&g.output, "output", "", "text (default) for people, json for scripts and agents. Also settable with KEMORY_OUTPUT.")
	flags.BoolVarP(&g.quiet, "quiet", "q", false, "Drop confirmations and hints. Warnings, errors and requested data still print.")

	// Grouped in --help: a flat alphabetical list tells a newcomer nothing
	// about which commands they need first.
	groups := []struct {
		id, title string
		cmds      []*cobra.Command
	}{
		{"start", "Getting started:", []*cobra.Command{newConnectCmd(g), newLoginCmd(g), newLogoutCmd(g), newDoctorCmd(g)}},
		{"vault", "Your vault:", []*cobra.Command{
			newRememberCmd(g), newRecallCmd(g), newAskCmd(g), newLsCmd(g), newShowCmd(g),
			newForgetCmd(g), newNamespacesCmd(g), newExportCmd(g), newStatsCmd(g), newBrowseCmd(g),
		}},
		{"identity", "Identity:", []*cobra.Command{newWhoamiCmd(g), newOrgsCmd(g), newUseCmd(g), newKeysCmd(g)}},
		{"integrations", "Integrations:", []*cobra.Command{newMCPCmd(g), newHookCmd(g)}},
		{"maintenance", "Maintenance:", []*cobra.Command{newUpgradeCmd(), newTelemetryCmd(g)}},
	}
	for _, grp := range groups {
		root.AddGroup(&cobra.Group{ID: grp.id, Title: grp.title})
		for _, c := range grp.cmds {
			c.GroupID = grp.id
			root.AddCommand(c)
		}
	}
	root.SetHelpCommandGroupID("maintenance")
	root.SetCompletionCommandGroupID("maintenance")
	installHelp(root)
	return root
}

// headed reports whether cmd opens with the mark and its name in a colour
// terminal. connect draws its own header; hook and mcp serve talk to agents,
// and completion prints a script.
func headed(cmd *cobra.Command) bool {
	switch strings.TrimPrefix(cmd.CommandPath(), cmd.Root().Name()+" ") {
	case "connect", "hook session-start", "hook capture", "mcp serve", "help":
		return false
	}
	return !strings.HasPrefix(cmd.CommandPath(), cmd.Root().Name()+" completion")
}

// Execute runs the CLI with args and returns the process exit code.
func Execute(args []string) int {
	root := newRoot()
	root.SetArgs(args)
	// --version and --help go through cobra's writers, not render.
	root.SetOut(render.Out)
	root.SetErr(render.Err)
	err := root.Execute()
	if err == nil {
		return exits.OK
	}
	code := exits.Code(err)
	var e *exits.Error
	if !errors.As(err, &e) && isUsageError(err) {
		code = exits.Usage
	}
	// An error with no message has said what happened already.
	if msg := err.Error(); msg != "" {
		render.Error(msg)
	}
	return code
}

// isUsageError recognises cobra's own invocation errors, which are plain
// errors rather than flag errors.
func isUsageError(err error) bool {
	msg := err.Error()
	return strings.HasPrefix(msg, "unknown command") ||
		strings.HasPrefix(msg, "accepts ") ||
		strings.HasPrefix(msg, "requires ")
}
