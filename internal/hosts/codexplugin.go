package hosts

import (
	"os"
	"path/filepath"
)

// codexPluginApp is the Kemory plugin's app id in OpenAI's plugin directory.
const codexPluginApp = "app-6a85afc169ac819185a36cd38c9fe432"

// CodexPluginInstall returns the file Codex keeps for the Kemory plugin it
// installed from OpenAI's plugin directory, or "". The plugin brings Kemory's
// MCP tools but no hooks, which the directory does not accept. Codex lists a
// directory plugin in no config file, only in its plugin cache.
func CodexPluginInstall() string {
	path := filepath.Join(codexHome(), "plugins", "cache", "openai-curated-remote", codexPluginApp, ".codex-remote-plugin-install.json")
	if _, err := os.Stat(path); err != nil {
		return ""
	}
	return path
}
