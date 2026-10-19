# kemory

The command-line client for [Kemory](https://kemory.sekondbrain.ai), a persistent memory vault for AI agents. You need a Kemory account; sign up at [kemory.sekondbrain.ai](https://kemory.sekondbrain.ai).

## Connect your MCP clients in one command

```bash
npx kemory connect
```

This signs you in through the browser, lists the AI clients on this machine (Claude Code, Claude Desktop, Codex, Cursor, VS Code, Gemini CLI, Windsurf, Cline, Kiro, Grok, Continue, Warp and others) with their Kemory status, and asks which to connect; Enter takes every one that is ready. Claude Code and Grok Build get the Kemory plugin, which saves sessions automatically; the others get an MCP entry. It then checks the connection. Restart the clients it lists afterwards. `--dry-run` shows what would change; `--host all` or `--host codex` skip the question. It is safe to re-run.

The npm package is a small launcher. On first run it downloads the native `kemory` binary for your platform (macOS, Linux or Windows x64), verifies its SHA-256 checksum and caches it.

## Other ways to install

```bash
npm install -g kemory
uvx kemory connect                          # Python / uv
brew install sekondbrainailabs/s9n/kemory   # macOS and Linux
```

## Common commands

```bash
npx kemory doctor         # check sign-in, network and MCP setup
npx kemory recall <query> # search your memories
npx kemory ask <question> # an answer with its evidence
npx kemory --help         # everything else
```

Source, issues and documentation: <https://github.com/SeKondBrainAILabs/kemory-cli>

## License

MIT
