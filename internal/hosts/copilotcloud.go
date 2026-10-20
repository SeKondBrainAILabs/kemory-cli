package hosts

import (
	"encoding/json"
	"path/filepath"
)

// Copilot's cloud agent works on a repository in a sandbox of its own, where
// no one signs in to Kemory. It runs the hooks in the repository's
// .github/hooks, with the repository's Agents secrets in their environment,
// so kemory's hooks there start kemory through npx and authenticate with the
// KEMORY_API_KEY secret.

// CopilotCloudHooksPath is the hooks file kemory owns in the repository at
// root.
func CopilotCloudHooksPath(root string) string {
	return filepath.Join(root, ".github", "hooks", "kemory.json")
}

// The cloud agent runs only the bash command of a hook.
type copilotCloudHandler struct {
	Type       string `json:"type"`
	Bash       string `json:"bash"`
	TimeoutSec int    `json:"timeoutSec"`
}

// InstallCopilotCloudHooks writes kemory's hooks into the repository at root,
// for env.
func InstallCopilotCloudHooks(root, env string) (string, error) {
	launch := Launch{Command: "npx", Args: []string{"-y", "kemory", "--env", env, "mcp", "serve"}}
	f := struct {
		Version int                              `json:"version"`
		Hooks   map[string][]copilotCloudHandler `json:"hooks"`
	}{1, map[string][]copilotCloudHandler{}}
	for _, e := range copilotHookEvents {
		command := hookCommand(launch, e.hook, "copilot")
		if e.as != "" {
			command += " --event " + e.as
		}
		// The first run downloads kemory.
		f.Hooks[e.event] = []copilotCloudHandler{{Type: "command", Bash: command, TimeoutSec: 60}}
	}
	path := CopilotCloudHooksPath(root)
	body, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return path, err
	}
	return path, writeFile(path, append(body, '\n'))
}
