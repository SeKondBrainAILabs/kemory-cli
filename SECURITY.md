# Security Policy

## Reporting a vulnerability

Please report security issues privately to **security@sekondbrain.ai** rather
than opening a public issue or pull request. We aim to acknowledge within 3
business days and keep you updated until the issue is resolved.

## Supported versions

Only the latest release receives security fixes. Check yours with
`kemory --version`.

## How the CLI handles credentials

- **Where they live.** `kemory login` writes `~/.kemory/credentials-<env>`
  (an OAuth access token and refresh token). The file is replaced atomically
  through a uniquely named temporary file and is created with mode `0600`; the
  `~/.kemory` directory is `0700`.
- **Where they go.** Tokens are sent only to the Kemory API host in that file
  and to the identity provider that issued them (for refresh). The CLI never
  follows an HTTP redirect with a token or refresh token attached: a redirect
  from the identity provider is reported as a configuration problem instead.
- **Residency cells.** When the API answers that your organisation's data lives
  in another region, the CLI retries once against that region's host, and only
  if the host is on a fixed allowlist compiled into the CLI. A host named in a
  server response is never trusted on its own.
- **API keys.** With `KEMORY_API_KEY` set, `kemory mcp serve` sends it as
  `X-API-Key` and does not read the credentials file.
- **MCP host configs.** `kemory mcp install` writes only the command that starts
  the bridge (`kemory --env <env> mcp serve`). No token or key is ever written
  into an MCP host's config file.
- **Opening the browser.** During `kemory login` the CLI opens the verification
  URL from the identity provider, and only if it is an `http(s)` URL.
