# kemory

The command-line client for [Kemory](https://kemory.sekondbrain.ai), a persistent memory vault for AI agents. You need a Kemory account; sign up at [kemory.sekondbrain.ai](https://kemory.sekondbrain.ai).

## Connect your MCP clients in one command

```bash
uvx kemory connect --host all
```

This signs you in through the browser, writes the Kemory MCP server entry into every MCP client installed on this machine (Claude Code, Claude Desktop, Codex, Cursor, VS Code, Gemini CLI, Windsurf, Cline, Kiro, Grok, Continue, Warp and others), and checks that the connection works. Restart the clients it lists afterwards. Without `--host`, only Claude Code is connected; `--host codex` and so on pick one client. It is safe to re-run.

This package contains the native `kemory` binary for your platform (macOS, Linux or Windows x64); it has no Python dependencies.

## Install

```bash
uv tool install kemory    # or: pipx install kemory
```

Also available with `npx kemory` and `brew install sekondbrainailabs/s9n/kemory`.

## Common commands

```bash
kemory doctor         # check sign-in, network and MCP setup
kemory recall <query> # search your memories
kemory ask <question> # an answer with its evidence
kemory --help         # everything else
```

Source, issues and documentation: <https://github.com/SeKondBrainAILabs/kemory-cli>

## License

MIT
