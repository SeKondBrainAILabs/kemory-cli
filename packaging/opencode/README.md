# opencode-kemory

[Kemory](https://kemory.sekondbrain.ai) memory for [OpenCode](https://opencode.ai). At the start of a session it adds your Kemory memory summary to the system prompt, and as the session goes it saves your prompts to Kemory.

## Install

Sign in to Kemory once, then add the plugin to `~/.config/opencode/opencode.json`:

```bash
npx -y kemory login
```

```json
{
  "plugin": ["opencode-kemory"]
}
```

OpenCode installs it the next time it starts. For Kemory's tools as well, run `npx -y kemory connect`, which also sets this up.

The plugin runs the `kemory` CLI when it is on your PATH, and `npx -y kemory` otherwise.

## Settings

| Variable | Effect |
|---|---|
| `KEMORY_CONTEXT=0` | Do not add the memory summary at session start |
| `KEMORY_AUTO_CAPTURE=0` | Do not save prompts |
| `KEMORY_ENV` | Kemory environment to use (default: prod) |
