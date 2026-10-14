# kemory

[![CI](https://github.com/SeKondBrainAILabs/kemory-cli/actions/workflows/ci.yml/badge.svg)](https://github.com/SeKondBrainAILabs/kemory-cli/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

The command-line client for [Kemory](https://kemory.sekondbrain.ai), a persistent
memory vault for AI agents. It signs you in, connects MCP clients such as Claude
Code, Claude Desktop and Cursor to your vault, and runs the MCP bridge they use.

> **Status:** this Go implementation covers every command of the released CLI
> and is not released yet. The released CLI is installed with
> `brew install sekondbrainailabs/s9n/kemory`, `npx kemory` or `uvx kemory`,
> and will switch to this build once it covers every command.

## Commands

| Command | What it does | Status |
|---|---|---|
| `kemory login` | Sign in through your browser (OAuth device flow), or `--local` with an API key | ✅ |
| `kemory logout` | Remove the cached sign-in for the selected environment | ✅ |
| `kemory whoami` | Show your account, active organisation and teams | ✅ |
| `kemory mcp serve` | The stdio MCP bridge an MCP client launches | ✅ |
| `kemory connect` | Sign in if needed, wire an MCP client and check the connection | ✅ |
| `kemory mcp install` | Write the kemory entry into MCP clients (`--host all` for every installed one) | ✅ |
| `kemory doctor` | Check sign-in, network, API and MCP clients; `--fix` wires or cleans them | ✅ |
| `kemory orgs`, `kemory use` | List your organisations and switch the active one | ✅ |
| `kemory remember`, `recall`, `ls`, `show`, `forget` | Store, search, read and delete memories | ✅ |
| `kemory ask` | Ask your vault a question and get an answer with its evidence | ✅ |
| `kemory namespaces`, `kemory export` | List namespaces, export the whole vault as JSON | ✅ |
| `kemory stats` | Recall usage and its token cost; `stats memories` for what is never recalled or rated down | ✅ |
| `kemory keys` | Create, rotate, approve, revoke, delete and list API keys for agents | ✅ |
| `kemory upgrade`, `telemetry`, `completion` | Check for a newer CLI, set your analytics preference, shell completion | ✅ |

Every command accepts `--output json` for scripts and agents (stdout then
carries only the JSON payload), `--quiet`, `--no-color` and `--env <name>`.

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

## Building from source

```bash
make build   # produces ./kemory
make check   # what CI runs: tests, lint, vulnerability scan, hygiene check
```

See [CONTRIBUTING.md](CONTRIBUTING.md) for how to work on the CLI and
[SECURITY.md](SECURITY.md) for how it handles credentials and how to report a
vulnerability.

## License

[MIT](LICENSE)
