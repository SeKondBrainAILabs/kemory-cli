// Package config holds the built-in environments and the per-environment
// credential cache at ~/.kemory/credentials-<env>.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ClientID is the pre-registered public PKCE client, named the same in every
// realm.
const ClientID = "kemory-cli"

// DefaultEnv is used when neither --env nor KEMORY_ENV is set.
const DefaultEnv = "prod"

// Environment is one target the CLI can sign in to.
type Environment struct {
	Issuer       string `json:"issuer"`
	KemoryURL    string `json:"kemory_url"`
	DashboardURL string `json:"dashboard_url"`
	ServerName   string `json:"server_name"`
}

// builtinEnvironments ship in every build. Internal environments (SeKondBrain
// staging) are never compiled in: they come from an environments file, see
// extraEnvironments.
var builtinEnvironments = map[string]Environment{
	"prod": {
		// Canonical issuer. A former issuer host 302-redirects device/token
		// POSTs, and the CLI deliberately never replays credential-bearing
		// POSTs across a redirect.
		Issuer: "https://accounts.sekondbrain.ai/realms/s9n",
		// Direct (bearer-auth) API host. It must equal the host the server
		// advertises as canonical in its OAuth/MCP discovery metadata.
		KemoryURL: "https://api.kemory.s9n.ai",
		// A browser-login host on a different domain from the API host.
		DashboardURL: "https://kemory.sekondbrain.ai",
		ServerName:   "kemory",
	},
	"local": {
		Issuer:       "http://localhost:8888/realms/s9n-mvp",
		KemoryURL:    "http://localhost:8100",
		DashboardURL: "http://localhost:3011",
		ServerName:   "kemory-local",
	},
	// The India residency cell. Same issuer and client as prod: residency
	// means data at rest in-region, so tokens work in both cells and only the
	// API host differs.
	"india": {
		Issuer:       "https://accounts.sekondbrain.ai/realms/s9n",
		KemoryURL:    "https://api.kemory.in.sekondbrain.ai",
		DashboardURL: "https://kemory.in.sekondbrain.ai",
		ServerName:   "kemory-india",
	},
}

// MigratedKemoryURLs maps hosts this CLI once shipped as a default to their
// replacement. Exact match only: a self-hosted URL that merely looks similar
// must be left alone.
var MigratedKemoryURLs = map[string]string{
	"https://api.kemory.sekondbrain.ai": "https://api.kemory.s9n.ai",
	"https://kemory.prod.apps.s9n.ai":   "https://api.kemory.s9n.ai",
}

// MigratedIssuers is the same exact-match migration for the cached issuer.
var MigratedIssuers = map[string]string{
	"https://accounts.prod.apps.s9n.ai/realms/s9n": "https://accounts.sekondbrain.ai/realms/s9n",
}

// environmentsFile is where extra environments are read from. Internal
// environments live there instead of in this public source tree.
func environmentsFile() string {
	if p := os.Getenv("KEMORY_ENVIRONMENTS_FILE"); p != "" {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".kemory", "environments.json")
}

// extraEnvironments reads {"name": {issuer, kemory_url, dashboard_url,
// server_name}} from the environments file. A missing or unreadable file means
// no extra environments.
func extraEnvironments() map[string]Environment {
	path := environmentsFile()
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var envs map[string]Environment
	if json.Unmarshal(data, &envs) != nil {
		return nil
	}
	return envs
}

// Environments returns the built-in environments plus any from the
// environments file. Built-ins win on a name clash.
func Environments() map[string]Environment {
	out := map[string]Environment{}
	for name, env := range extraEnvironments() {
		out[strings.ToLower(name)] = env
	}
	for name, env := range builtinEnvironments {
		out[name] = env
	}
	return out
}

// ResolveEnv picks the environment: --env flag > KEMORY_ENV > prod.
func ResolveEnv(flag string) (string, error) {
	env := flag
	if env == "" {
		env = os.Getenv("KEMORY_ENV")
	}
	if env == "" {
		env = DefaultEnv
	}
	env = strings.ToLower(env)
	envs := Environments()
	if _, ok := envs[env]; !ok {
		names := make([]string, 0, len(envs))
		for name := range envs {
			names = append(names, name)
		}
		sort.Strings(names)
		return "", fmt.Errorf("unknown env '%s' (choose: %s). Extra environments can be defined in %s",
			env, strings.Join(names, ", "), environmentsFile())
	}
	return env, nil
}

// Profile is an environment with per-key environment-variable overrides
// applied, for self-hosted and advanced use.
type Profile struct {
	Environment
	ClientID string
}

// EnvProfile returns the profile for env, which must already be resolved.
func EnvProfile(env string) Profile {
	e := Environments()[env]
	p := Profile{Environment: e, ClientID: ClientID}
	if v := os.Getenv("KEMORY_OIDC_ISSUER"); v != "" {
		p.Issuer = v
	}
	if v := os.Getenv("KEMORY_URL"); v != "" {
		p.KemoryURL = v
	}
	if v := os.Getenv("KEMORY_CLI_CLIENT_ID"); v != "" {
		p.ClientID = v
	}
	if v := os.Getenv("KEMORY_DASHBOARD_URL"); v != "" {
		p.DashboardURL = v
	}
	return p
}
