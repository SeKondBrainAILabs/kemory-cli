package cli

import (
	"net/http"
	"strings"
	"testing"

	"github.com/SeKondBrainAILabs/kemory-cli/internal/exits"
)

func TestStatsSummaryPrintsTheWindowAndCost(t *testing.T) {
	setHome(t)
	newFakeVault(t, map[string]func(http.ResponseWriter){
		"/api/v1/analytics/recall/summary": jsonReply(200, `{"window_days": 30, "recalls": 4, "empty_recalls": 1, "first_recorded_at": "2026-10-01T00:00:00Z",
			"tokens_injected": 12345, "by_namespace": [{"namespace": "project:kemory", "recalls": 4, "tokens_injected": 12345}]}`),
	})
	code, out, _ := run(t, "stats")
	for _, want := range []string{"30 days (recording since 2026-10-01)", "1 (25%)", "12,345", "By namespace · 30d"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q (exit %d):\n%s", want, code, out)
		}
	}
	if strings.Contains(strings.ToLower(out), "saved") {
		t.Fatal("stats must never claim savings")
	}
}

func TestStatsWithNoRecallsExplainsWhatThatMeans(t *testing.T) {
	setHome(t)
	newFakeVault(t, map[string]func(http.ResponseWriter){
		"/api/v1/analytics/recall/summary": jsonReply(200, `{"window_days": 7, "recalls": 0}`),
	})
	_, out, _ := run(t, "stats", "--days", "7")
	if !strings.Contains(out, "No recalls recorded in the last 7 days") || !strings.Contains(out, "not that your memories are unhelpful") {
		t.Fatalf("got:\n%s", out)
	}
}

func TestStatsMemoriesViewsAndGuards(t *testing.T) {
	setHome(t)
	v := newFakeVault(t, map[string]func(http.ResponseWriter){
		"/api/v1/analytics/recall/memories": jsonReply(200, `{"window_days": 30, "total_memories": 10, "never_recalled": 4,
			"covers_full_window": false, "first_recorded_at": "2026-10-10T00:00:00Z", "rows": []}`),
	})
	_, out, _ := run(t, "stats", "memories", "--never-recalled")
	if !strings.Contains(out, "Not enough history yet") || !strings.Contains(out, "fully covered on 2026-11-09") {
		t.Fatalf("the count must be withheld until the window is covered:\n%s", out)
	}
	if v.methods["/api/v1/analytics/recall/memories"] != http.MethodGet {
		t.Fatal("no request made")
	}
	for _, args := range [][]string{
		{"stats", "memories", "--never-recalled", "--rated-down"},
		{"stats", "memories", "--never-recalled", "--sort", "recalls"},
		{"stats", "memories", "--rated-down", "--sort", "age"},
		{"stats", "memories", "--sort", "size"},
		{"stats", "--days", "400"},
	} {
		if code, _, _ := run(t, args...); code != exits.Usage {
			t.Fatalf("%v: got %d", args, code)
		}
	}
}

func TestKeysCreatePrintsTheKeyAndWarnsWhenPending(t *testing.T) {
	setHome(t)
	v := newFakeVault(t, map[string]func(http.ResponseWriter){
		"/api/v1/agents": jsonReply(201, `{"agent_id": "ag-1", "api_key": "km_secret", "status": "pending_approval"}`),
	})
	code, out, errOut := run(t, "-q", "keys", "create", "--name", "ci", "--for", "cursor-cloud")
	if code != 0 || !strings.Contains(out, "km_secret") || !strings.Contains(errOut, "kemory keys approve ag-1") {
		t.Fatalf("got %d\n%s\n%s", code, out, errOut)
	}
	body := v.bodies["/api/v1/agents"]
	if !strings.HasPrefix(body["agent_description"].(string), "[agent=cursor-cloud] ") {
		t.Fatalf("label not tagged: %v", body)
	}
	if scopes := body["declared_scopes"].([]any); len(scopes) != 1 {
		t.Fatalf("keys must be read-only without --write: %v", scopes)
	}
}

func TestKeysListShowsTheAgentLabel(t *testing.T) {
	setHome(t)
	newFakeVault(t, map[string]func(http.ResponseWriter){
		"/api/v1/agents": jsonReply(200, `[{"agent_id": "ag-1", "agent_name": "ci", "agent_description": "[agent=cursor-cloud] x", "status": "active"}]`),
	})
	_, out, _ := run(t, "keys", "list")
	if !strings.Contains(out, "cursor-cloud") || !strings.Contains(out, "active") {
		t.Fatalf("got:\n%s", out)
	}
}

func TestKeysRevokeAsksAndDeleteNeedsRevokeFirst(t *testing.T) {
	setHome(t)
	v := newFakeVault(t, map[string]func(http.ResponseWriter){
		"/api/v1/agents/ag-1/revoke": jsonReply(200, `{}`),
		"/api/v1/agents/ag-1":        jsonReply(400, `{"detail": "not revoked"}`),
	})
	withStdin(t, "\n")
	if code, _, _ := run(t, "keys", "revoke", "ag-1"); code == 0 {
		t.Fatal("declining must not revoke")
	}
	if _, called := v.methods["/api/v1/agents/ag-1/revoke"]; called {
		t.Fatal("revoke was sent without confirmation")
	}
	if code, out, _ := run(t, "keys", "revoke", "ag-1", "--yes"); code != 0 || !strings.Contains(out, "revoked") {
		t.Fatalf("got %d %q", code, out)
	}
	if code, _, errOut := run(t, "keys", "delete", "ag-1"); code != exits.Conflict || !strings.Contains(errOut, "revoked first") {
		t.Fatalf("got %d %q", code, errOut)
	}
}

func TestTelemetry(t *testing.T) {
	setHome(t)
	v := newFakeVault(t, map[string]func(http.ResponseWriter){
		"/api/v1/user/telemetry-preference": jsonReply(200, `{"opted_in": false}`),
	})
	if code, out, _ := run(t, "telemetry", "off"); code != 0 || !strings.Contains(out, "telemetry: off") {
		t.Fatalf("got %d %q", code, out)
	}
	if v.methods["/api/v1/user/telemetry-preference"] != http.MethodPut || v.bodies["/api/v1/user/telemetry-preference"]["opted_in"] != false {
		t.Fatalf("request: %v %v", v.methods, v.bodies)
	}
	if code, _, _ := run(t, "telemetry", "maybe"); code != exits.Usage {
		t.Fatalf("bad state: %d", code)
	}
}

func TestBrowsePointsAtTheDashboard(t *testing.T) {
	setHome(t)
	_, out, _ := run(t, "browse")
	if !strings.Contains(out, "https://kemory.sekondbrain.ai/memories") {
		t.Fatalf("got:\n%s", out)
	}
}

func TestUpgradeNamesTheInstallersCommand(t *testing.T) {
	setHome(t)
	fakeReleases(t, `[{"tag_name": "cli-v9.9.9", "assets": [{"name": "kemory-linux-x64.tar.gz"}]}]`)
	old := executablePath
	t.Cleanup(func() { executablePath = old })
	cases := map[string]string{
		"/opt/homebrew/Cellar/kemory/0.7.0/libexec/kemory": "brew upgrade kemory",
		"/home/a/.local/share/uv/tools/kemory/bin/kemory":  "uv tool upgrade kemory",
		"/home/a/.local/pipx/venvs/kemory/bin/kemory":      "pipx upgrade kemory",
		"/usr/lib/node_modules/kemory/bin/kemory":          "npm install -g kemory@latest",
		"/usr/local/bin/kemory":                            "Download 9.9.9",
	}
	for exe, want := range cases {
		executablePath = func() (string, error) { return exe, nil }
		code, out, _ := run(t, "upgrade")
		if code != 0 || !strings.Contains(out, want) {
			t.Fatalf("%s: got %d\n%s", exe, code, out)
		}
	}
	t.Setenv("KEMORY_LAUNCHER", "npx")
	if _, out, _ := run(t, "upgrade"); !strings.Contains(out, "npx always runs the newest release") {
		t.Fatalf("npx: %s", out)
	}
}

func TestCompletionScripts(t *testing.T) {
	setHome(t)
	for _, shell := range []string{"bash", "zsh", "fish", "powershell"} {
		code, out, _ := run(t, "completion", shell)
		if code != 0 || !strings.Contains(out, "kemory") {
			t.Fatalf("%s: got %d", shell, code)
		}
	}
}

func TestUpgradeAcceptsThePythonCLIsYesFlag(t *testing.T) {
	setHome(t)
	fakeReleases(t, `[{"tag_name": "cli-v9.9.9", "assets": [{"name": "kemory-linux-x64.tar.gz"}]}]`)
	for _, flag := range []string{"--yes", "-y"} {
		if code, _, errOut := run(t, "upgrade", flag); code != 0 {
			t.Fatalf("%s: got %d %q", flag, code, errOut)
		}
	}
}

func TestUpgradeCheckDoesNotClaimANewerVersionItCannotCompare(t *testing.T) {
	setHome(t) // version.Version is "dev" in tests
	fakeReleases(t, `[{"tag_name": "cli-v0.6.14", "assets": [{"name": "kemory-linux-x64.tar.gz"}]}]`)
	_, out, _ := run(t, "upgrade", "--check")
	if strings.Contains(out, "A newer version is available") || !strings.Contains(out, "Could not tell") {
		t.Fatalf("got:\n%s", out)
	}
}
