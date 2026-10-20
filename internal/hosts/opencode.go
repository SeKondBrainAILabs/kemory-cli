package hosts

import (
	_ "embed"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/SeKondBrainAILabs/kemory-cli/internal/jsonedit"
)

// OpenCode installs the npm packages in its config's plugin list, and loads
// every file in its plugins directory, at startup. Kemory's plugin, either
// way, loads the memory summary into the system prompt and saves the
// session's prompts, through `kemory hook`. OpenCode asks no approval for it.

//go:embed opencode.js
var openCodePlugin string

// commandPlaceholder is replaced with the command that starts kemory.
const commandPlaceholder = "__KEMORY_COMMAND__"

// openCodeDir is OpenCode's config directory: $XDG_CONFIG_HOME/opencode or
// ~/.config/opencode, on every OS.
func openCodeDir() string {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "opencode")
	}
	return filepath.Join(home(), ".config", "opencode")
}

// OpenCodePluginPath is where kemory's OpenCode plugin lives.
func OpenCodePluginPath() string {
	return filepath.Join(openCodeDir(), "plugins", "kemory.js")
}

// openCodePluginFor is the plugin source that starts kemory the way launch
// does.
func openCodePluginFor(launch Launch) (string, error) {
	command := []string{launch.Command}
	args := launch.Args
	if n := len(args); n >= 2 && args[n-2] == "mcp" && args[n-1] == "serve" {
		args = args[:n-2]
	}
	command = append(command, args...)
	quoted, err := json.Marshal(command)
	if err != nil {
		return "", err
	}
	return strings.Replace(openCodePlugin, commandPlaceholder, string(quoted), 1), nil
}

// openCodeHeader is the comment that opens the plugin kemory connect writes.
const openCodeHeader = "// Kemory memory for OpenCode. Written by `kemory connect`; it is replaced\n" +
	"// when you connect again, so keep your own changes in another file.\n"

// OpenCodeNPMPlugin is the source of the opencode-kemory npm package: the
// same plugin, finding kemory when it runs.
func OpenCodeNPMPlugin() string {
	source := strings.Replace(openCodePlugin, openCodeHeader,
		"// Kemory memory for OpenCode, as the opencode-kemory npm package. Add\n"+
			"// \"opencode-kemory\" to the \"plugin\" list in opencode.json.\n", 1)
	source = strings.Replace(source, commandPlaceholder, "kemoryCommand()", 1)
	source = strings.Replace(source, `import { join } from "node:path"`,
		"import { existsSync } from \"node:fs\"\nimport { delimiter, join } from \"node:path\"", 1)
	return source + `
// kemoryCommand is the kemory CLI on PATH, or else the kemory npm package
// through npx. KEMORY_ENV picks the Kemory environment, as for the CLI.
function kemoryCommand() {
  // A .cmd shim runs only through a shell, which npx gets on Windows.
  const name = process.platform === "win32" ? "kemory.exe" : "kemory"
  for (const dir of (process.env.PATH ?? "").split(delimiter)) {
    if (dir && existsSync(join(dir, name))) return [join(dir, name)]
  }
  return ["npx", "-y", "kemory"]
}
`
}

// openCodePackage is the npm package OpenCode installs kemory's plugin from.
const openCodePackage = "opencode-kemory"

// fromNPM reports whether the npm package starts kemory the way launch does.
// It finds kemory on PATH or runs it through npx, for the default
// environment; any other launch keeps a plugin file of its own.
func fromNPM(launch Launch) bool {
	args := strings.Join(launch.Args, " ")
	switch launch.Command {
	case "kemory":
		return args == "--env prod mcp serve"
	case "npx":
		return args == "-y kemory --env prod mcp serve"
	}
	return false
}

// InstallOpenCodePlugin adds the opencode-kemory package to OpenCode's plugin
// list, or for a launch it cannot follow writes kemory's plugin into the
// plugins directory. Either one replaces the other, since OpenCode would load
// both.
func InstallOpenCodePlugin(launch Launch) (string, error) {
	config := ResolveConfig("opencode")
	if !fromNPM(launch) {
		path := OpenCodePluginPath()
		source, err := openCodePluginFor(launch)
		if err != nil {
			return path, err
		}
		if err := setOpenCodePackage(config, false); err != nil {
			return path, err
		}
		return path, writeFile(path, []byte(source))
	}
	if err := setOpenCodePackage(config, true); err != nil {
		return config, err
	}
	if openCodeFileInstalled() {
		if err := os.Remove(OpenCodePluginPath()); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return config, err
		}
	}
	return config, nil
}

// setOpenCodePackage adds the package to, or takes it out of, the plugin list
// in the OpenCode config at path.
func setOpenCodePackage(path string, add bool) error {
	cfg, list, err := openCodePlugins(path)
	if err != nil || hasOpenCodePackage(list) == add {
		return err
	}
	if add {
		list = append(list, openCodePackage)
	} else {
		kept := list[:0]
		for _, p := range list {
			if !isOpenCodePackage(p) {
				kept = append(kept, p)
			}
		}
		list = kept
	}
	if len(list) == 0 {
		cfg.Delete("plugin")
	} else if err := cfg.SetValue("plugin", list); err != nil {
		return err
	}
	return writeJSON(path, cfg)
}

// openCodePlugins reads the config at path and its plugin list.
func openCodePlugins(path string) (*jsonedit.Object, []any, error) {
	cfg, _, err := readJSON(path, "mcp")
	if err != nil {
		return nil, nil, err
	}
	var list []any
	if raw, ok := cfg.Get("plugin"); ok && string(raw) != "null" {
		if err := json.Unmarshal(raw, &list); err != nil {
			return nil, nil, invalid(path, "has a plugin that is not a JSON list")
		}
	}
	return cfg, list, nil
}

// isOpenCodePackage reports whether plugin names the package, at any version.
func isOpenCodePackage(plugin any) bool {
	name, _ := plugin.(string)
	return name == openCodePackage || strings.HasPrefix(name, openCodePackage+"@")
}

func hasOpenCodePackage(list []any) bool {
	for _, p := range list {
		if isOpenCodePackage(p) {
			return true
		}
	}
	return false
}

// openCodeFileInstalled reports whether kemory's plugin file is there.
func openCodeFileInstalled() bool {
	data, err := os.ReadFile(OpenCodePluginPath())
	return err == nil && strings.Contains(string(data), "export const KemoryMemory")
}

// OpenCodePluginInstalled reports whether OpenCode loads kemory's plugin: the
// package, or a plugin file for a launch the package cannot follow. A file an
// older kemory wrote for the default launch is due to be replaced.
func OpenCodePluginInstalled() bool {
	if _, list, err := openCodePlugins(ResolveConfig("opencode")); err == nil && hasOpenCodePackage(list) {
		return true
	}
	if !openCodeFileInstalled() {
		return false
	}
	data, _ := os.ReadFile(OpenCodePluginPath())
	_, rest, _ := strings.Cut(string(data), "const KEMORY = ")
	line, _, _ := strings.Cut(rest, "\n")
	var command []string
	if json.Unmarshal([]byte(line), &command) != nil || len(command) == 0 {
		return true
	}
	return !fromNPM(Launch{Command: command[0], Args: append(command[1:], "mcp", "serve")})
}

// OpenCodeMemoryPath is the file that gives OpenCode kemory's plugin.
func OpenCodeMemoryPath() string {
	if openCodeFileInstalled() {
		if _, list, err := openCodePlugins(ResolveConfig("opencode")); err != nil || !hasOpenCodePackage(list) {
			return OpenCodePluginPath()
		}
	}
	return ResolveConfig("opencode")
}
