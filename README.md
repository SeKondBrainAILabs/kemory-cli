# kemory

[![CI](https://github.com/SeKondBrainAILabs/kemory-cli/actions/workflows/ci.yml/badge.svg)](https://github.com/SeKondBrainAILabs/kemory-cli/actions/workflows/ci.yml)
[![npm](https://img.shields.io/npm/v/kemory.svg)](https://www.npmjs.com/package/kemory)
[![PyPI](https://img.shields.io/pypi/v/kemory.svg)](https://pypi.org/project/kemory/)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

The command-line client for [Kemory](https://kemory.sekondbrain.ai), a persistent
memory vault for AI agents. It signs you in, connects MCP clients such as Claude
Code, Codex, Cursor and VS Code to your vault, and runs the MCP bridge they use.

## Quick start

You need a Kemory account; sign up at [kemory.sekondbrain.ai](https://kemory.sekondbrain.ai).

1. Connect every MCP client on this machine. Nothing to install first:

   ```bash
   npx kemory connect --host all   # Node.js 18+
   uvx kemory connect --host all   # or with uv
   ```

   Your browser opens to sign in, then the kemory entry is written into each
   client's config and the connection is checked.
2. Restart the clients it lists. Warp picks the change up without a restart.
3. Check the setup end to end at any time with `kemory doctor`.

Name one client instead of `all` with `--host claude-code`, `--host codex` and
so on; [MCP clients](#mcp-clients) lists them.

## Install

```bash
brew install sekondbrainailabs/s9n/kemory
npm install -g kemory
uv tool install kemory
```

Builds exist for macOS (Apple silicon and Intel), Linux (arm64 and x64) and
Windows (x64). They are attached to each
[GitHub release](https://github.com/SeKondBrainAILabs/kemory-cli/releases)
with checksums and build provenance
(`gh attestation verify <file> --repo SeKondBrainAILabs/kemory-cli`).
`kemory upgrade` tells you whether a newer release exists and how to update
the copy you installed.

## Commands

| Command | What it does |
|---|---|
| `kemory login` | Sign in through your browser (OAuth device flow), or `--local` with an API key |
| `kemory logout` | Remove the cached sign-in for the selected environment |
| `kemory whoami` | Show your account, active organisation and teams |
| `kemory mcp serve` | The stdio MCP bridge an MCP client launches |
| `kemory connect` | Sign in if needed, wire an MCP client and check the connection |
| `kemory mcp install` | Write the kemory entry into MCP clients (`--host all` for every installed one) |
| `kemory doctor` | Check sign-in, network, API and MCP clients; `--fix` wires or cleans them |
| `kemory orgs`, `kemory use` | List your organisations and switch the active one |
| `kemory remember`, `recall`, `ls`, `show`, `forget` | Store, search, read and delete memories |
| `kemory ask` | Ask your vault a question and get an answer with its evidence |
| `kemory namespaces`, `kemory export` | List namespaces, export the whole vault as JSON |
| `kemory stats` | Recall usage and its token cost; `stats memories` for what is never recalled or rated down |
| `kemory keys` | Create, rotate, approve, revoke, delete and list API keys for agents |
| `kemory upgrade`, `telemetry`, `completion` | Check for a newer CLI, set your analytics preference, shell completion |

Every command accepts `--output json` for scripts and agents (stdout then
carries only the JSON payload), `--quiet`, `--no-color` and `--env <name>`.

## MCP clients

`kemory connect --host <name>` and `kemory mcp install --host <name>` write the
entry for one client, and `--host all` writes it for every client installed on
this machine. The entry only starts `kemory mcp serve`; no token or key is
written into the client's config. Everything else in the file is left as it was.

| `--host` | Client | Config file (macOS and Linux) |
|---|---|---|
| `claude-code` | Claude Code | `~/.claude.json` |
| `claude-desktop` | Claude Desktop | `~/Library/Application Support/Claude/claude_desktop_config.json` |
| `codex` | OpenAI Codex | `~/.codex/config.toml`, or `$CODEX_HOME/config.toml` |
| `cursor` | Cursor | `~/.cursor/mcp.json` |
| `vscode` | VS Code (Copilot agent mode) | `~/Library/Application Support/Code/User/mcp.json`, `~/.config/Code/User/mcp.json` |
| `gemini-cli` | Gemini CLI | `~/.gemini/settings.json` |
| `antigravity` | Google Antigravity | `~/.gemini/config/mcp_config.json` |
| `windsurf` | Windsurf / Devin Desktop | `~/.config/devin/mcp_config.json`, or `~/.codeium/windsurf/mcp_config.json` for older installs |
| `cline` | Cline | the VS Code extension's `cline_mcp_settings.json`, or `~/.cline/data/settings/cline_mcp_settings.json` |
| `kiro` | Kiro | `~/.kiro/settings/mcp.json` |
| `grok` | Grok CLI | `~/.grok/config.toml` |
| `continue` | Continue | `~/.continue/mcpServers/kemory.json` |
| `warp` | Warp | `~/.warp/.mcp.json` |

On Windows the files under `Library/Application Support` and `.config` live in
`%APPDATA%` instead. Grok also reads `~/.claude.json`, so the `claude-code`
entry reaches it too.

Claude Desktop rewrites its config file from memory while it runs, which drops
an entry added in the meantime. Quit it completely (⌘Q on macOS) before
connecting it; kemory leaves its config alone while it is open and says so.

## Exit codes

Scripts and agents can act on why a command failed:

| Code | Meaning |
|---|---|
| 0 | Success |
| 1 | Failure that fits no category below |
| 2 | Invalid invocation |
| 3 | Not signed in, or the sign-in was rejected — run `kemory login` |
| 4 | Kemory could not be reached; nothing ran on the server |
| 5 | Signed in but not allowed (403) |
| 6 | Not found (404) |
| 7 | Conflict with existing state (409) |
| 8 | Rate limited (429) |
| 9 | Kemory is up but temporarily unavailable (503) |

## Configuration

| Variable | Effect |
|---|---|
| `KEMORY_ENV` | Environment to use: `prod` (default), `india`, `local`, or one from the environments file |
| `KEMORY_URL` | Use this Kemory API host instead of the environment's |
| `KEMORY_OIDC_ISSUER`, `KEMORY_CLI_CLIENT_ID` | Identity provider and client for self-hosted Kemory |
| `KEMORY_API_KEY` | `mcp serve` authenticates with this key instead of your sign-in |
| `KEMORY_HTTP_TIMEOUT` | Seconds `mcp serve` waits for Kemory (default 120) |
| `KEMORY_OUTPUT` | `text` or `json`, like `--output` |
| `NO_COLOR` | Disable colour |
| `KEMORY_HEADLESS=1` | Never try to open a browser |

Sign-ins are stored per environment in `~/.kemory/credentials-<env>`. Extra
environments can be defined in `~/.kemory/environments.json` (or the file named
by `KEMORY_ENVIRONMENTS_FILE`):

```json
{"myenv": {"issuer": "https://idp.example.com/realms/x", "kemory_url": "https://kemory.example.com", "dashboard_url": "https://app.example.com", "server_name": "kemory-myenv"}}
```

## What the CLI sends

Commands talk to the selected environment's Kemory API, and `login` and token
refreshes to its sign-in server (`accounts.sekondbrain.ai` for prod). Each API
request carries your access token, the active organisation, the CLI version, OS and
architecture (`X-Kemory-Client`), and the name of the command being run
(`X-Kemory-Command`, never its arguments). Kemory uses the last two for product
analytics; `kemory telemetry off` turns that off for your account.
`kemory doctor` and `kemory upgrade` also read the public release list on
GitHub to compare versions.

## Uninstall

1. Remove the kemory entries from your MCP clients' config files (the
   [MCP clients](#mcp-clients) table names each file); for Continue, delete
   `~/.continue/mcpServers/kemory.json`.
2. Run `kemory logout`, or delete `~/.kemory/` to drop every saved sign-in.
3. Uninstall the CLI the way you installed it: `brew uninstall kemory`,
   `npm uninstall -g kemory` or `uv tool uninstall kemory`.

## Building from source

```bash
make build   # produces ./kemory
make check   # what CI runs: tests, lint, vulnerability scan, hygiene check
```

See [CONTRIBUTING.md](CONTRIBUTING.md) for how to work on the CLI and
[SECURITY.md](SECURITY.md) for how it handles credentials and how to report a
vulnerability. Questions and bugs go to
[GitHub issues](https://github.com/SeKondBrainAILabs/kemory-cli/issues).

## License

[MIT](LICENSE)
