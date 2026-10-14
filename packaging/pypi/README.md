# kemory

The command-line client for [Kemory](https://kemory.sekondbrain.ai), a persistent memory vault for AI agents.

## Connect an MCP client in one command

```bash
uvx kemory connect
```

This signs you in through the browser, writes the Kemory MCP server entry for the MCP clients installed on this machine (Claude Code, Codex, Cursor, VS Code and others), and checks that the connection works. It is safe to re-run.

This package contains the native `kemory` binary for your platform; it has no Python dependencies.

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
