package cli

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/SeKondBrainAILabs/kemory-cli/internal/ui"
)

// Help in a colour terminal carries the guided screen's look: the
// SeKondBrain mark and name, section titles in Kemory's colour, and names
// that stand out from their descriptions. Anywhere else it is cobra's plain
// text, which scripts and tests read.

// styledHelpWanted reports whether help may be styled: stdout is a terminal
// and nothing asked for plain text.
var styledHelpWanted = func(cmd *cobra.Command) bool {
	f, ok := cmd.OutOrStdout().(*os.File)
	if !ok || !term.IsTerminal(int(f.Fd())) {
		return false
	}
	noColor, _ := cmd.Flags().GetBool("no-color")
	return !noColor && os.Getenv("NO_COLOR") == "" && os.Getenv("KEMORY_ACCESSIBLE") != "1"
}

// installHelp makes every command's help styled where it may be.
func installHelp(root *cobra.Command) {
	plain := root.HelpFunc()
	root.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		if !styledHelpWanted(cmd) {
			plain(cmd, args)
			return
		}
		fmt.Fprint(cmd.OutOrStdout(), styledHelp(cmd))
	})
}

// flagLine splits a line of pflag's usage into the flag and its description.
var flagLine = regexp.MustCompile(`^(\s+)(-\S.*?)(\s{2,})(\S.*)$`)

// helpWidth is the terminal's width for wrapping help, at most 100.
func helpWidth(cmd *cobra.Command) int {
	if f, ok := cmd.OutOrStdout().(*os.File); ok {
		if w, _, err := term.GetSize(int(f.Fd())); err == nil && w > 40 {
			return min(w-2, 100)
		}
	}
	return 100
}

// wrap breaks text into lines of at most width columns.
func wrap(text string, width int) []string {
	var lines []string
	line := ""
	for _, word := range strings.Fields(text) {
		if line != "" && len(line)+1+len(word) > width {
			lines = append(lines, line)
			line = ""
		}
		if line != "" {
			line += " "
		}
		line += word
	}
	return append(lines, line)
}

func styledHelp(cmd *cobra.Command) string {
	width := helpWidth(cmd)
	var b strings.Builder
	line := func(s string) { b.WriteString(s + "\n") }
	section := func(title string) {
		line("")
		line(" " + ui.Section(title))
	}

	b.WriteString("\n")
	if !cmd.HasParent() {
		line(" " + ui.Mark() + " " + ui.Wordmark())
		line("   " + ui.Tagline())
	} else {
		line(" " + ui.Mark() + " " + ui.Bold(cmd.CommandPath()))
		about := strings.TrimSpace(cmd.Long)
		if about == "" {
			about = cmd.Short
		}
		for i, para := range strings.Split(about, "\n\n") {
			if i > 0 {
				line("")
			}
			for _, l := range wrap(para, width-3) {
				line("   " + l)
			}
		}
	}

	// The root's command list says how to use it; a command's usage line
	// shows its arguments.
	if cmd.HasParent() {
		section("Usage")
		if cmd.Runnable() {
			line("   " + cmd.UseLine())
		}
		if cmd.HasAvailableSubCommands() {
			line("   " + cmd.CommandPath() + " <command>")
		}
		if len(cmd.Aliases) > 0 {
			line("   " + ui.Dim("also: "+strings.Join(cmd.Aliases, ", ")))
		}
	}

	if ex := strings.TrimRight(cmd.Example, "\n"); ex != "" {
		section("Examples")
		for _, l := range strings.Split(ex, "\n") {
			line("   " + strings.TrimLeft(l, " "))
		}
	}

	if cmd.HasAvailableSubCommands() {
		nameWidth := 0
		for _, c := range cmd.Commands() {
			if c.IsAvailableCommand() || c.Name() == "help" {
				nameWidth = max(nameWidth, len(c.Name()))
			}
		}
		row := func(c *cobra.Command) {
			pad := strings.Repeat(" ", 3+nameWidth+2)
			for i, l := range wrap(c.Short, width-len(pad)) {
				if i == 0 {
					line("   " + ui.Bold(fmt.Sprintf("%-*s", nameWidth, c.Name())) + "  " + l)
				} else {
					line(pad + l)
				}
			}
		}
		listed := false
		for _, grp := range cmd.Groups() {
			section(strings.TrimSuffix(grp.Title, ":"))
			for _, c := range cmd.Commands() {
				if c.GroupID == grp.ID && (c.IsAvailableCommand() || c.Name() == "help") {
					row(c)
				}
			}
			listed = true
		}
		var rest []*cobra.Command
		for _, c := range cmd.Commands() {
			if c.GroupID == "" && c.IsAvailableCommand() {
				rest = append(rest, c)
			}
		}
		if len(rest) > 0 {
			title := "Commands"
			if listed {
				title = "More commands"
			}
			section(title)
			for _, c := range rest {
				row(c)
			}
		}
	}

	// pflag indents flags by 2 and wraps descriptions to the width it is
	// given; one more space lines them up with the commands.
	flags := func(title, usages string) {
		usages = strings.TrimRight(usages, "\n")
		if usages == "" {
			return
		}
		section(title)
		for _, l := range strings.Split(usages, "\n") {
			// --help is how this page was reached; the footer names it.
			if strings.Contains(l, "--help ") || strings.HasSuffix(l, "--help") {
				continue
			}
			if m := flagLine.FindStringSubmatch(l); m != nil {
				line(" " + m[1] + ui.Bold(m[2]) + m[3] + m[4])
			} else {
				line(" " + l)
			}
		}
	}
	flags("Flags", cmd.LocalFlags().FlagUsagesWrapped(width-1))
	if cmd.HasParent() {
		flags("Global flags", cmd.InheritedFlags().FlagUsagesWrapped(width-1))
	}

	if cmd.HasAvailableSubCommands() {
		line("")
		line(" " + ui.Dim("Run "+cmd.CommandPath()+" <command> --help for details."))
	}
	line("")
	return b.String()
}
