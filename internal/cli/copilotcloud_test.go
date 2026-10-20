package cli

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/SeKondBrainAILabs/kemory-cli/internal/exits"
)

// fakeRepo stands in for git in a working tree whose origin is remote, and
// for gh storing secrets, which it records or refuses with ghErr.
func fakeRepo(t *testing.T, remote string, ghErr error) (root string, secrets map[string]string) {
	t.Helper()
	root = t.TempDir()
	secrets = map[string]string{}
	oldGit, oldSecret := gitOutput, setAgentsSecret
	gitOutput = func(_ string, args ...string) (string, error) {
		if args[0] == "rev-parse" {
			return root, nil
		}
		return remote, nil
	}
	setAgentsSecret = func(repo, name, value string) error {
		if ghErr != nil {
			return ghErr
		}
		secrets[repo+" "+name] = value
		return nil
	}
	t.Cleanup(func() { gitOutput, setAgentsSecret = oldGit, oldSecret })
	return root, secrets
}

// fakeAgents serves the agents API, by method and path, recording each call.
func fakeAgents(t *testing.T, list string) (*httptest.Server, *[]string) {
	t.Helper()
	var mu sync.Mutex
	var calls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		calls = append(calls, r.Method+" "+r.URL.Path+" "+string(body))
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Header.Get("Authorization") != "Bearer at":
			w.WriteHeader(http.StatusUnauthorized)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/agents":
			_, _ = io.WriteString(w, list)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/agents":
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"agent_id": "ag-new", "api_key": "kemory_new", "status": "pending_approval"}`)
		case strings.HasSuffix(r.URL.Path, "/rotate-key"):
			_, _ = io.WriteString(w, `{"agent_id": "`+agentID+`", "api_key": "kemory_rotated", "status": "active"}`)
		case strings.HasSuffix(r.URL.Path, "/approve"):
			_, _ = io.WriteString(w, `{"status": "active"}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	saveCreds(t, srv.URL)
	return srv, &calls
}

func TestConnectCopilotCloudWritesHooksAndStoresAnApprovedKey(t *testing.T) {
	setHome(t)
	root, secrets := fakeRepo(t, "git@github.com:acme/widgets.git", nil)
	srv, calls := fakeAgents(t, `[]`)
	t.Setenv("KEMORY_URL", srv.URL)

	code, out, errOut := run(t, "connect", "copilot-cloud")
	if code != 0 {
		t.Fatalf("got %d\n%s\n%s", code, out, errOut)
	}
	created := strings.Join(*calls, "\n")
	if !strings.Contains(created, `"agent_name":"copilot-cloud acme/widgets"`) || !strings.Contains(created, "memory:write") ||
		!strings.Contains(created, "/api/v1/agents/ag-new/approve") {
		t.Fatalf("calls:\n%s", created)
	}
	if secrets["acme/widgets KEMORY_API_KEY"] != "kemory_new" || secrets["acme/widgets COPILOT_MCP_KEMORY_API_KEY"] != "kemory_new" {
		t.Fatalf("secrets: %v", secrets)
	}
	if strings.Contains(out, "kemory_new") {
		t.Fatal("a key stored as a secret is not printed")
	}

	var hooks struct {
		Hooks map[string][]struct {
			Bash       string `json:"bash"`
			TimeoutSec int    `json:"timeoutSec"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal([]byte(readFile(t, filepath.Join(root, ".github", "hooks", "kemory.json"))), &hooks); err != nil {
		t.Fatal(err)
	}
	if got := hooks.Hooks["sessionStart"]; len(got) != 1 || got[0].Bash != "npx -y kemory --env prod hook session-start --agent copilot" {
		t.Fatalf("hooks: %+v", hooks)
	}
	if got := hooks.Hooks["sessionEnd"]; len(got) != 1 || !strings.HasSuffix(got[0].Bash, "hook capture --agent copilot --event SessionEnd") {
		t.Fatalf("hooks: %+v", hooks)
	}

	for _, want := range []string{`"url": "` + srv.URL + `/mcp/v1"`, `"X-API-Key": "$COPILOT_MCP_KEMORY_API_KEY"`, "add 127.0.0.1:"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

func TestConnectCopilotCloudRotatesTheRepositorysKey(t *testing.T) {
	setHome(t)
	_, secrets := fakeRepo(t, "https://github.com/acme/widgets", nil)
	srv, calls := fakeAgents(t, `[{"agent_id": "`+agentID+`", "agent_name": "copilot-cloud acme/widgets"}]`)
	t.Setenv("KEMORY_URL", srv.URL)

	if code, out, errOut := run(t, "connect", "copilot-cloud"); code != 0 {
		t.Fatalf("got %d\n%s\n%s", code, out, errOut)
	}
	if got := strings.Join(*calls, "\n"); strings.Contains(got, "POST /api/v1/agents ") || !strings.Contains(got, "/rotate-key") {
		t.Fatalf("calls:\n%s", got)
	}
	if secrets["acme/widgets KEMORY_API_KEY"] != "kemory_rotated" {
		t.Fatalf("secrets: %v", secrets)
	}
}

func TestConnectCopilotCloudPrintsTheKeyWhenGhCannotStoreIt(t *testing.T) {
	setHome(t)
	fakeRepo(t, "git@github.com:acme/widgets.git", errors.New("gh not found"))
	srv, _ := fakeAgents(t, `[]`)
	t.Setenv("KEMORY_URL", srv.URL)

	code, out, errOut := run(t, "connect", "copilot-cloud")
	if code != 0 || !strings.Contains(out, "api_key : kemory_new") || !strings.Contains(errOut, "gh not found") {
		t.Fatalf("got %d\n%s\n%s", code, out, errOut)
	}
}

func TestConnectCopilotCloudNeedsAGitHubRepository(t *testing.T) {
	setHome(t)
	fakeRepo(t, "https://gitlab.com/acme/widgets.git", nil)
	if code, _, errOut := run(t, "connect", "copilot-cloud"); code != exits.Usage || !strings.Contains(errOut, "--repo") {
		t.Fatalf("got %d %s", code, errOut)
	}
}

func TestHooksAuthenticateWithKemoryAPIKey(t *testing.T) {
	setHome(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer kemory_key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = io.WriteString(w, contextReply)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("KEMORY_URL", srv.URL)
	t.Setenv("KEMORY_API_KEY", "kemory_key")

	withStdin(t, `{"sessionId": "s1", "source": "new"}`)
	if code, out, _ := run(t, "hook", "session-start", "--agent", "copilot"); code != 0 || !strings.Contains(out, `"additionalContext"`) {
		t.Fatalf("got %d %q", code, out)
	}
}
