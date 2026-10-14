# kemory

The command-line client for [Kemory](https://kemory.sekondbrain.ai), a persistent memory vault for AI agents.

## Connect an MCP client in one command

```bash
npx kemory connect
```

This signs you in through the browser, writes the Kemory MCP server entry for the MCP clients installed on this machine (Claude Code, Claude Desktop, Cursor and others), and checks that the connection works. It is safe to re-run.

The npm package is a small launcher. On first run it downloads the native `kemory` binary for your platform (macOS, Linux or Windows), verifies its SHA-256 checksum and caches it.

## Other ways to install

```bash
uvx kemory connect                          # Python / uv
brew install sekondbrainailabs/s9n/kemory   # macOS and Linux
```

## Common commands

```bash
npx kemory login          # browser sign-in
npx kemory whoami         # show the signed-in account
npx kemory doctor         # check credentials, API reachability and MCP setup
npx kemory --help         # everything else
```

## License

MIT
