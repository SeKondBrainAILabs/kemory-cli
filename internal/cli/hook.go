package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"github.com/SeKondBrainAILabs/kemory-cli/internal/api"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/auth"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/hook"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/render"
)

// A hook runs inside someone else's agent, at session start or after every
// turn. So it never fails the agent: every error is swallowed (and shown only
// with KEMORY_HOOK_DEBUG=1), it exits 0, and it gives up after a deadline
// rather than hold the session up.

// hookAgents are the agents whose payload and output shapes kemory knows.
var hookAgents = []string{"claude-code", "codex", "cursor", "gemini-cli", "grok", "opencode", "text"}

// hookDeadline is how long a hook may take, KEMORY_HOOK_TIMEOUT seconds or 2.
func hookDeadline() time.Duration {
	if n, err := strconv.ParseFloat(os.Getenv("KEMORY_HOOK_TIMEOUT"), 64); err == nil && n > 0 {
		return time.Duration(n * float64(time.Second))
	}
	return 2 * time.Second
}

func hookDebug(format string, args ...any) {
	if os.Getenv("KEMORY_HOOK_DEBUG") == "1" {
		fmt.Fprintf(render.Err, "kemory hook: "+format+"\n", args...)
	}
}

// withDeadline runs work and reports whether it finished before the deadline.
// Work still running at the deadline is abandoned; only after a true result
// may the caller read what work wrote.
func withDeadline(work func()) bool {
	done := make(chan struct{})
	go func() {
		defer close(done)
		work()
	}()
	select {
	case <-done:
		return true
	case <-time.After(hookDeadline()):
		hookDebug("gave up after %s", hookDeadline())
		return false
	}
}

// hookPayload reads the JSON the agent pipes to its hook, if any.
func hookPayload() []byte {
	if stdinIsTerminal() {
		return nil
	}
	data, _ := io.ReadAll(io.LimitReader(Stdin, 1<<20))
	return data
}

func validAgent(agent string) bool {
	return contains(hookAgents, agent)
}

func newHookCmd(g *globals) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "hook",
		Short: "Memory hooks a coding agent runs: load context at session start, save the session at the end.",
		Long: "Commands a coding agent's hooks call; `kemory connect` sets them up. They\n" +
			"never fail the agent: errors are silent (KEMORY_HOOK_DEBUG=1 shows them), the\n" +
			"exit status is 0, and each gives up after KEMORY_HOOK_TIMEOUT seconds (default 2).",
		Args: cobra.NoArgs,
	}
	cmd.AddCommand(newHookSessionStartCmd(g), newHookCaptureCmd(g))
	return cmd
}

func newHookSessionStartCmd(g *globals) *cobra.Command {
	var agent string
	cmd := &cobra.Command{
		Use:   "session-start",
		Short: "Print the user's Kemory context in the form the agent injects at session start.",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			if !validAgent(agent) || os.Getenv("KEMORY_CONTEXT") == "0" {
				return nil
			}
			var source struct {
				Source string `json:"source"`
			}
			_ = json.Unmarshal(hookPayload(), &source)
			var out []byte
			finished := withDeadline(func() {
				creds, err := auth.ValidCredentials(g.env, false)
				if err != nil || creds == nil {
					hookDebug("not signed in to %s: %v", g.env, err)
					return
				}
				resp, err := api.Request(creds, http.MethodGet, hook.ContextPath(), nil, hookDeadline())
				if err != nil || resp.Status != http.StatusOK {
					hookDebug("context request failed: %v (status %d)", err, respStatus(resp))
					return
				}
				out, err = hook.Output(agent, hook.BuildContext(resp.Body, source.Source == "compact"))
				if err != nil {
					out = nil
				}
			})
			if finished && len(out) > 0 {
				fmt.Fprintln(render.Out, string(out))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&agent, "agent", "claude-code", "The agent running the hook, which decides the output shape: "+joinAgents()+".")
	return cmd
}

func newHookCaptureCmd(g *globals) *cobra.Command {
	var agent, transcript, sessionID, event string
	cmd := &cobra.Command{
		Use:   "capture",
		Short: "Save the user's new prompts from a session transcript to Kemory, redacted.",
		Long: "Save the user's prompts from the session transcript that are not stored yet,\n" +
			"with credentials redacted, as one memory in user:sessions. Prints nothing.\n" +
			"The agent's hook payload on stdin names the transcript and session; the\n" +
			"flags fill in what an agent does not send. Turn it off with KEMORY_AUTO_CAPTURE=0.",
		Args: cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			if !validAgent(agent) || os.Getenv("KEMORY_AUTO_CAPTURE") == "0" {
				return nil
			}
			e := hook.ParseEvent(hookPayload())
			if transcript != "" {
				e.TranscriptPath = transcript
			}
			if sessionID != "" {
				e.SessionID = sessionID
			}
			if event != "" {
				e.Name = event
			}
			withDeadline(func() { capture(g.env, agent, e) })
			return nil
		},
	}
	cmd.Flags().StringVar(&agent, "agent", "claude-code", "The agent running the hook: "+joinAgents()+".")
	cmd.Flags().StringVar(&transcript, "transcript", "", "Session transcript (JSON Lines), when the payload does not name one.")
	cmd.Flags().StringVar(&sessionID, "session-id", "", "Session id, when the payload does not carry one.")
	cmd.Flags().StringVar(&event, "event", "", "Hook event (Stop saves once 3 new prompts exist; anything else saves what is pending).")
	return cmd
}

func capture(env, agent string, e hook.Event) {
	if e.TranscriptPath == "" {
		hookDebug("no transcript")
		return
	}
	turns, err := hook.UserTurns(e.TranscriptPath)
	if err != nil {
		hookDebug("transcript: %v", err)
		return
	}
	c := hook.Plan(e, turns, agent)
	if c == nil {
		hookDebug("nothing new to store")
		return
	}
	creds, err := auth.ValidCredentials(env, false)
	if err != nil || creds == nil {
		hookDebug("not signed in to %s: %v", env, err)
		return
	}
	resp, err := api.Post(creds, "/api/v1/memories", c.Body, hookDeadline())
	if err != nil || resp.Status < 200 || resp.Status > 299 {
		// The high-water mark stays put, so these turns are tried again.
		hookDebug("store failed: %v (status %d)", err, respStatus(resp))
		return
	}
	if err := c.Stored(); err != nil {
		hookDebug("state: %v", err)
	}
}

func respStatus(resp *api.Response) int {
	if resp == nil {
		return 0
	}
	return resp.Status
}

func joinAgents() string {
	out := ""
	for i, a := range hookAgents {
		if i > 0 {
			out += ", "
		}
		out += a
	}
	return out
}
