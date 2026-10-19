package hosts

import (
	"errors"
	"io/fs"
	"os"

	"github.com/SeKondBrainAILabs/kemory-cli/internal/jsonedit"
)

func jsonServerNames(path, key string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	cfg, err := jsonedit.Parse(data)
	if err != nil {
		return nil
	}
	servers, err := cfg.Object(key)
	if err != nil {
		return nil
	}
	return servers.Keys()
}

// readJSON returns the config at path and its servers object. A missing file
// is an empty config.
func readJSON(path, key string) (cfg, servers *jsonedit.Object, err error) {
	cfg = jsonedit.NewObject()
	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		if cfg, err = jsonedit.Parse(data); err != nil {
			return nil, nil, invalid(path, "is not valid JSON")
		}
	case !errors.Is(err, fs.ErrNotExist):
		return nil, nil, err
	}
	if servers, err = cfg.Object(key); err != nil {
		return nil, nil, invalid(path, "has a "+key+" that is not a JSON object")
	}
	return cfg, servers, nil
}

func writeJSONEntry(path string, h host, name string, launch Launch) error {
	key := h.key
	cfg, servers, err := readJSON(path, key)
	if err != nil {
		return err
	}
	// Structs, not maps, so the entry reads type, command, args, env.
	var entry any = struct {
		Command string            `json:"command"`
		Args    []string          `json:"args"`
		Env     map[string]string `json:"env"`
	}{launch.Command, launch.Args, map[string]string{}}
	switch {
	case h.typed:
		entry = struct {
			Type    string   `json:"type"`
			Command string   `json:"command"`
			Args    []string `json:"args"`
		}{"stdio", launch.Command, launch.Args}
	case h.local:
		entry = struct {
			Type    string   `json:"type"`
			Command []string `json:"command"`
			Enabled bool     `json:"enabled"`
		}{"local", append([]string{launch.Command}, launch.Args...), true}
	}
	if err := servers.SetValue(name, entry); err != nil {
		return err
	}
	if err := cfg.SetObject(key, servers); err != nil {
		return err
	}
	return writeJSON(path, cfg)
}

// removeJSONEntries returns path when nothing but the emptied servers object
// would remain, so the caller deletes the file instead.
func removeJSONEntries(path, key string, names []string) (string, error) {
	if _, err := os.Stat(path); err != nil {
		return "", err
	}
	cfg, servers, err := readJSON(path, key)
	if err != nil {
		return "", err
	}
	for _, n := range names {
		servers.Delete(n)
	}
	if servers.Len() == 0 && (cfg.Len() == 0 || (cfg.Len() == 1 && cfg.Keys()[0] == key)) {
		return path, nil
	}
	if err := cfg.SetObject(key, servers); err != nil {
		return "", err
	}
	return "", writeJSON(path, cfg)
}

func writeJSON(path string, cfg *jsonedit.Object) error {
	body, err := cfg.Marshal("")
	if err != nil {
		return err
	}
	return writeFile(path, append(body, '\n'))
}
