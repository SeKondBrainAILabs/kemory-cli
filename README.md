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

1. Connect your AI clients. Nothing to install first:

   ```bash
   npx kemory connect   # Node.js 18+
   uvx kemory connect   # or with uv
   ```

   Your browser opens to sign in. kemory then lists the AI clients on this
   machine with their Kemory status and asks which to connect; Enter takes
   every one that is ready. Claude Code and Grok Build get the Kemory plugin,
   the others an MCP entry, and the connection is checked.
2. Restart the clients it lists. Warp picks the change up without a restart.
3. Check the setup end to end at any time with `kemory doctor`.

`--dry-run` shows what would change without changing anything. In scripts,
name the clients instead: `--host all`, or `--host claude-code`, `--host codex`
and so on ([MCP clients](#mcp-clients) lists them); kemory then asks nothing.

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
| `kemory hook session-start`, `hook capture` | What a coding agent's hooks run: load your Kemory context when a session starts, save the session when it ends ([Hooks](#hooks)) |
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

`kemory connect --host <name>` and `kemory mcp install --host <name>` connect
one client, and `--host all` connects every client installed on this machine.

For Claude Code and Grok Build they install the
[Kemory plugin](https://github.com/SeKondBrainAILabs/kemory-plugin) through the
client's own `claude` or `grok` command. Its hooks save each session to Kemory,
and in Claude Code also load your memory when a session starts. Without that
command, or for an environment other than prod, they write the MCP entry
instead.

For every other client they write an MCP entry. The entry only starts
`kemory mcp serve`; no token or key is written into the client's config, and
everything else in the file is left as it was.

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

### One connection per client

Each client should reach Kemory one way. Two ways give it every Kemory tool
twice, which costs context in every session.

| Client | Connect it with |
|---|---|
| claude.ai, Claude Desktop and Claude mobile | The claude.ai connector, added from the Kemory dashboard |
| Claude Code and Grok Build | The Kemory plugin, which `kemory connect` installs |
| Everything else in the table above | `kemory connect --host <name>` |

`connect` and `mcp install` write nothing for a client that already has
Kemory: Claude Code or Grok Build with the Kemory plugin enabled, or Claude
Desktop when your account has a claude.ai connector in use. `kemory doctor` reports a kemory entry
that duplicates either one, and `kemory doctor --fix` removes it.

Claude Desktop rewrites its config file from memory while it runs, which drops
an entry added or removed in the meantime. When its config has to change while
it is open, `connect`, `mcp install` and `doctor --fix` offer to quit it, make
the change and open it again (`doctor --fix --yes` does so without asking).
Run them from Terminal or iTerm rather than the terminal inside the Claude app,
which would close with it; kemory notices and says so.

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

## Hooks

Coding agents with hooks can give a session your Kemory context when it starts
and save it when it ends. `kemory connect` sets this up where it can; these are
the commands the hooks call.

| Command | Hook event | Does |
|---|---|---|
| `kemory hook session-start --agent <agent>` | session start | Prints your namespace summaries from Kemory, led by a short instruction on using it, in the shape the agent injects. `--agent` is `claude-code`, `codex`, `gemini-cli`, `cursor`, or `text` for plain output. |
| `kemory hook capture --agent <agent>` | after each turn, before compaction, session end | Reads the transcript the agent's payload names (or `--transcript`) and saves your prompts that are not stored yet as one memory. After a turn it waits until there are 3 new prompts; at compaction and session end it saves whatever is pending. Prints nothing. |

Neither can hold up the agent: any error is silent, the exit status is always
0, and each gives up after `KEMORY_HOOK_TIMEOUT` seconds (default 2).
`KEMORY_HOOK_DEBUG=1` prints why a hook did nothing.

Capture sends only your own prompts, never the agent's replies or tool output:
at most the last 12 new prompts and 8000 characters per memory, with
credential-shaped text (API keys, tokens, passwords, private keys) replaced by
`[REDACTED]`. They are stored in the `user:sessions` namespace, visible only to
you. It records what it has stored in `~/.kemory/.captured/<session>`, the same
file the Kemory plugin uses, so a session is never stored twice.

| Variable | Effect |
|---|---|
| `KEMORY_CONTEXT=0` | Turn off session-start context |
| `KEMORY_CONTEXT_DEPTH`, `KEMORY_CONTEXT_MAX_CHARS`, `KEMORY_CONTEXT_NAMESPACES` | Summary depth (default `l3`), character budget (default 4000), namespaces to include (comma-separated; default all) |
| `KEMORY_AUTO_CAPTURE=0` | Turn off capture |
| `KEMORY_CAPTURE_NAMESPACE`, `KEMORY_CAPTURE_VISIBILITY` | Where captures go (default `user:sessions`, `user-private`) |
| `KEMORY_CAPTURE_MAX_TURNS`, `KEMORY_CAPTURE_MIN_NEW_TURNS` | Prompts per memory (default 12), new prompts a per-turn capture waits for (default 3) |

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
