package hosts

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"

	"github.com/BurntSushi/toml"
)

// GrokPluginConfig returns ~/.grok/config.toml when Grok Build has the Kemory
// plugin installed and enabled, or "". The plugin brings its own MCP server.
func GrokPluginConfig() string {
	dir := filepath.Join(home(), ".grok")
	data, err := os.ReadFile(filepath.Join(dir, "installed-plugins", "registry.json"))
	if err != nil {
		return ""
	}
	var registry struct {
		Repos map[string]struct {
			Plugins map[string]json.RawMessage `json:"plugins"`
		} `json:"repos"`
	}
	if json.Unmarshal(data, &registry) != nil {
		return ""
	}
	installed := false
	for _, repo := range registry.Repos {
		if _, ok := repo.Plugins["kemory"]; ok {
			installed = true
		}
	}
	if !installed {
		return ""
	}
	path := filepath.Join(dir, "config.toml")
	var cfg struct {
		Plugins struct {
			Enabled []string `toml:"enabled"`
		} `toml:"plugins"`
	}
	if _, err := toml.DecodeFile(path, &cfg); err != nil || !slices.Contains(cfg.Plugins.Enabled, "kemory") {
		return ""
	}
	return path
}
