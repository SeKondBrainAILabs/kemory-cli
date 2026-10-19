package hosts

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenCodeEntriesAreLocalCommandsUnderMCP(t *testing.T) {
	home := setHome(t)
	path := ResolveConfig("opencode")
	if path != filepath.Join(home, ".config", "opencode", "opencode.json") {
		t.Fatalf("path: %s", path)
	}
	mustWrite(t, path, `{"$schema": "https://opencode.ai/config.json", "model": "anthropic/claude-sonnet-5-5"}`)
	if err := WriteEntry("opencode", path, "kemory", LaunchFor("prod")); err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Model string `json:"model"`
		MCP   map[string]struct {
			Type    string   `json:"type"`
			Command []string `json:"command"`
			Enabled bool     `json:"enabled"`
		} `json:"mcp"`
	}
	if err := json.Unmarshal([]byte(read(t, path)), &cfg); err != nil {
		t.Fatal(err)
	}
	entry := cfg.MCP["kemory"]
	if cfg.Model == "" || entry.Type != "local" || !entry.Enabled ||
		strings.Join(entry.Command, " ") != "kemory --env prod mcp serve" {
		t.Fatalf("got %+v", cfg)
	}
}

func TestOpenCodePluginStartsKemoryTheWayTheEntryDoes(t *testing.T) {
	setHome(t)
	t.Setenv("KEMORY_LAUNCHER", "npx")
	path, err := InstallOpenCodePlugin(LaunchFor("india"))
	if err != nil {
		t.Fatal(err)
	}
	source := read(t, path)
	if !strings.Contains(source, `const KEMORY = ["npx","-y","kemory","--env","india"]`) || strings.Contains(source, commandPlaceholder) {
		t.Fatalf("command not filled in:\n%s", source)
	}
	if !OpenCodePluginInstalled() {
		t.Fatal("installed plugin not detected")
	}
	if Installed("opencode") {
		t.Fatal("the plugin kemory wrote is not the app")
	}
}

// harness drives the plugin the way OpenCode does, against a fake kemory
// that records each call.
const harness = `
import { KemoryMemory } from %q
const client = { session: { messages: async () => ({ data: [
  { info: { role: "user" }, parts: [{ type: "text", text: "fix the build" }, { type: "text", text: "noise", synthetic: true }] },
  { info: { role: "assistant" }, parts: [{ type: "text", text: "done" }] },
  { info: { role: "user" }, parts: [{ type: "file" }, { type: "text", text: "ship it" }] },
] }) } }
const hooks = await KemoryMemory({ client, directory: "/w" })
for (const _ of [1, 2]) {
  const output = { system: ["base"] }
  await hooks["experimental.chat.system.transform"]({ sessionID: "s1", model: {} }, output)
  if (output.system.join("|") !== "base|CONTEXT") throw new Error("system: " + output.system)
}
await hooks.event({ event: { type: "session.idle", properties: { sessionID: "s1" } } })
await hooks.dispose()
`

const fakeKemory = `
const fs = require("node:fs")
const args = process.argv.slice(2).join(" ")
let input = ""
process.stdin.on("data", (d) => (input += d))
process.stdin.on("end", () => {
  let line = args
  if (args.includes("hook capture")) {
    const payload = JSON.parse(input)
    line += " " + payload.hook_event_name + " " + payload.session_id + " " + payload.cwd + " " +
      fs.readFileSync(payload.transcript_path, "utf8").trim().split("\n").join(" ")
  }
  fs.appendFileSync(process.env.KEMORY_FAKE_LOG, line + "\n")
  if (args.includes("hook session-start")) process.stdout.write("CONTEXT\n")
})
`

func TestOpenCodePluginLoadsContextOnceAndCapturesUserPrompts(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	setHome(t)
	dir := t.TempDir()
	fake := filepath.Join(dir, "fake-kemory.cjs")
	mustWrite(t, fake, fakeKemory)
	path, err := InstallOpenCodePlugin(Launch{Command: node, Args: []string{fake, "mcp", "serve"}})
	if err != nil {
		t.Fatal(err)
	}
	// The plugin is an ES module; node treats a .mjs copy as one.
	module := filepath.Join(dir, "kemory.mjs")
	mustWrite(t, module, read(t, path))
	script := filepath.Join(dir, "harness.mjs")
	mustWrite(t, script, strings.Replace(harness, "%q", jsonString("file://"+filepath.ToSlash(module)), 1))
	log := filepath.Join(dir, "calls.log")
	cmd := exec.Command(node, script)
	cmd.Env = append(os.Environ(), "KEMORY_FAKE_LOG="+log)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	want := `hook session-start --agent text
hook capture --agent opencode Stop s1 /w {"role":"user","content":"fix the build"} {"role":"user","content":"ship it"}
hook capture --agent opencode SessionEnd s1 /w {"role":"user","content":"fix the build"} {"role":"user","content":"ship it"}
`
	if got := strings.ReplaceAll(read(t, log), "\r\n", "\n"); got != want {
		t.Fatalf("calls:\n%s\nwant:\n%s", got, want)
	}
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
