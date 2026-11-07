# Contributing

Thanks for helping improve the Kemory CLI. By taking part you agree to the
[Code of Conduct](CODE_OF_CONDUCT.md).

## Ground rules

- Open an issue before a large change so we can agree on the approach.
- **Exit codes are a contract.** Scripts and agents act on them
  (`internal/exits`). Add new codes; never renumber or reuse one.
- **The credentials file format is shared** with other Kemory clients
  (`internal/config`). Add fields; never rename or remove one.
- **`kemory mcp serve` owns stdout.** MCP hosts read JSON-RPC from it, so
  anything else written there breaks the connection. Diagnostics go to stderr.
- **No internal references in files.** Ticket ids, links into internal tools
  and internal hostnames are rejected by `scripts/check-public-hygiene.sh`,
  which CI runs on every pull request.

## Architecture

Code lives in `cmd/kemory` and the packages under `internal/`. Packages form
layers, and an import may only point down the list below. Keep the graph
acyclic and add an import only when the table allows it; if you need one it
doesn't allow, change this section in the same pull request and explain why.

| Package | May import (from `internal/`) |
| --- | --- |
| `cmd/kemory` | `cli` |
| `cli` | `api`, `auth`, `bridge`, `config`, `exits`, `hook`, `hosts`, `releases`, `render`, `residency`, `ui`, `version` |
| `bridge` | `api`, `auth`, `config`, `residency`, `version` |
| `api` | `auth`, `config`, `exits`, `residency`, `version` |
| `auth` | `config`, `exits` |
| `hosts` | `jsonedit` |
| `config`, `exits`, `hook`, `jsonedit`, `releases`, `render`, `residency`, `ui`, `version` | nothing |

In particular:

- Only `cli` wires the packages together. `hosts` and `hook` never import
  `cli` or `api`, and no package imports `cli`.
- Leaf packages stay leaves. Pass what they need as arguments instead of
  importing another `internal/` package.

## Development

You need Go (the version in `go.mod`) and `make`.

```bash
make build     # ./kemory
make test      # go test -race ./...
make lint      # golangci-lint (the version CI uses)
make fmt       # gofumpt + goimports
make vuln      # govulncheck
make snapshot  # GoReleaser build of all release targets into dist/
```

Run `make check` before opening a pull request; CI runs the same checks on
Linux, macOS and Windows.

### Tests

Tests never touch a real Kemory account. Each test points `HOME` at a temporary
directory and talks to an in-process fake API or identity provider
(`net/http/httptest`). Keep it that way: a test that needs the network or your
real `~/.kemory` is a bug.

## Pull requests

- Use [Conventional Commits](https://www.conventionalcommits.org/) for the
  title, for example `fix(login): report a stale issuer instead of asking to log in again`.
- Explain the problem and the behaviour change, and how you tested it.
- Keep a pull request to one change. Refactors go in their own pull request.
