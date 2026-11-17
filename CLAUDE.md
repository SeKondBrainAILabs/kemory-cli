# CLAUDE.md

Kemory CLI, a Go program. Code lives in `cmd/kemory` and `internal/`.
`CONTRIBUTING.md` is the source for the rules below; read it before a change.

## Architecture

Packages form layers and an import may only point down. The full table is in
`CONTRIBUTING.md#architecture`; in short:

- `cmd/kemory` imports `cli`. Only `cli` wires packages together, and nothing
  imports `cli`.
- `bridge` imports `api`, `auth`, `config`, `residency`, `version`.
- `api` imports `auth`, `config`, `exits`, `residency`, `version`.
- `auth` imports `config`, `exits`. `hosts` imports `jsonedit`.
- `config`, `exits`, `hook`, `jsonedit`, `releases`, `render`, `residency`,
  `ui`, `version` are leaves: pass them what they need as arguments.

To add an import the table forbids, change the table in the same PR.

## Commands

- `make test` runs `go test -race ./...`. `make check` runs hygiene, lint, test
  and vuln, which is what CI runs.
- Run one package with `go test ./internal/<pkg>`.

## Tests

Tests never touch a real account or the network. Each test sets `HOME` to a
temporary directory and talks to an in-process fake API or identity provider
built on `net/http/httptest`. A test that needs the network or the real
`~/.kemory` is a bug.

## Traps

- **`packaging/opencode/index.js` is generated.** Don't edit it by hand. After
  changing the plugin that `kemory connect` writes, run
  `UPDATE_OPENCODE_PLUGIN=1 go test ./internal/hosts`; otherwise
  `TestOpenCodeNPMPluginIsCurrent` fails.
- **Public hygiene.** The repo is public. `scripts/check-public-hygiene.sh`
  (`make hygiene`) rejects ticket ids, links to internal trackers and internal
  hostnames in any tracked file, including this one. Run it before pushing.
- **Exit codes are a contract** (`internal/exits`): add new ones, never
  renumber or reuse.
- **The credentials file format is shared** with other Kemory clients
  (`internal/config`): add fields, never rename or remove one.
- **`kemory mcp serve` owns stdout** for JSON-RPC. Write diagnostics to stderr.
- Commits and PR titles use Conventional Commits, for example
  `fix(login): report a stale issuer`.
