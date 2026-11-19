package cli

import (
	"path/filepath"
	"testing"

	"github.com/SeKondBrainAILabs/kemory-cli/internal/hosts"
)

// besidePluginMachine has Codex installed with a config that holds a kemory
// entry when duplicate is set, and with kemory's memory hooks when hooks is.
func besidePluginMachine(t *testing.T, duplicate, hooks bool) {
	t.Helper()
	home := setHome(t)
	installApp(t, home, ".codex")
	config := "[mcp_servers.other]\ncommand = \"other\"\n"
	if duplicate {
		config += "[mcp_servers.kemory]\ncommand = \"kemory\"\nargs = [\"mcp\", \"serve\"]\n"
	}
	writeFile(t, filepath.Join(home, ".codex", "config.toml"), config)
	if hooks {
		if _, err := hosts.InstallCodexHooks(hosts.Launch{Command: "kemory"}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestPlanBesidePluginListsTheStepsAToolsOnlyPluginHostStillNeeds(t *testing.T) {
	for _, tc := range []struct {
		name            string
		duplicate, hook bool
		want            func(config, hooks string) string
	}{
		{"duplicate and no hooks", true, false, func(c, h string) string {
			return "remove the duplicate kemory entry from " + c + " and add memory hooks to " + h
		}},
		{"duplicate with hooks", true, true, func(c, _ string) string {
			return "remove the duplicate kemory entry from " + c
		}},
		{"no duplicate and no hooks", false, false, func(_, h string) string {
			return "add memory hooks to " + h
		}},
		{"no duplicate with hooks", false, true, func(string, string) string { return "" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			besidePluginMachine(t, tc.duplicate, tc.hook)
			want := tc.want(hosts.ResolveConfig("codex"), hosts.CodexHooksPath())
			if got := planBesidePlugin("codex", "kemory"); got != want {
				t.Fatalf("got %q, want %q", got, want)
			}
		})
	}
}

func TestPlannedForAToolsOnlyPluginHostSaysWhatItWouldStillChange(t *testing.T) {
	besidePluginMachine(t, true, false)
	covered := map[string]string{"codex": "connected through the Kemory plugin"}
	got := planned("prod", "codex", "kemory", covered)
	want := "already connected through the Kemory plugin; would " + planBesidePlugin("codex", "kemory")
	if got.detail != want || !got.ok {
		t.Fatalf("got %+v, want detail %q", got, want)
	}

	besidePluginMachine(t, false, true)
	got = planned("prod", "codex", "kemory", covered)
	if want := "already connected through the Kemory plugin — nothing to do"; got.detail != want {
		t.Fatalf("got %q, want %q", got.detail, want)
	}
}
