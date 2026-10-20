// Kemory memory for OpenCode, as the opencode-kemory npm package. Add
// "opencode-kemory" to the "plugin" list in opencode.json.
//
// At the start of a session it adds your Kemory memory summary to the system
// prompt, and as the session goes it saves your prompts to Kemory, through
// `kemory hook`. Turn either off with KEMORY_CONTEXT=0 or KEMORY_AUTO_CAPTURE=0.
import { execFile } from "node:child_process"
import { writeFile, rm } from "node:fs/promises"
import { tmpdir } from "node:os"
import { existsSync } from "node:fs"
import { delimiter, join } from "node:path"

const KEMORY = kemoryCommand()

function kemory(args, input) {
  return new Promise((resolve) => {
    const child = execFile(
      KEMORY[0],
      [...KEMORY.slice(1), ...args],
      // On Windows npx and uvx are .cmd shims, which only a shell runs. A full
      // path is run directly, so its spaces need no quoting.
      { timeout: 15000, shell: process.platform === "win32" && !/[\\/]/.test(KEMORY[0]) },
      (err, stdout) => resolve(err ? "" : stdout),
    )
    child.stdin?.end(input ?? "")
  })
}

export const KemoryMemory = async ({ client, directory }) => {
  const context = new Map()
  const sessions = new Set()

  const capture = async (sessionID, event) => {
    const res = await client.session.messages({ path: { id: sessionID } }).catch(() => undefined)
    const lines = []
    for (const message of res?.data ?? []) {
      if (message.info?.role !== "user") continue
      const text = (message.parts ?? [])
        .filter((p) => p.type === "text" && !p.synthetic && !p.ignored)
        .map((p) => p.text)
        .join(" ")
        .trim()
      if (text) lines.push(JSON.stringify({ role: "user", content: text }))
    }
    if (lines.length === 0) return
    const transcript = join(tmpdir(), `kemory-opencode-${sessionID}.jsonl`)
    await writeFile(transcript, lines.join("\n") + "\n", { mode: 0o600 })
    await kemory(
      ["hook", "capture", "--agent", "opencode"],
      JSON.stringify({ session_id: sessionID, hook_event_name: event, transcript_path: transcript, cwd: directory }),
    )
    await rm(transcript, { force: true })
  }

  return {
    "experimental.chat.system.transform": async (input, output) => {
      if (!input.sessionID) return
      sessions.add(input.sessionID)
      if (!context.has(input.sessionID)) {
        context.set(input.sessionID, kemory(["hook", "session-start", "--agent", "text"]))
      }
      const text = (await context.get(input.sessionID)).trim()
      if (text) output.system.push(text)
    },
    event: async ({ event }) => {
      if (event.type === "session.idle") await capture(event.properties.sessionID, "Stop")
      if (event.type === "session.compacted") await capture(event.properties.sessionID, "PreCompact")
    },
    dispose: async () => {
      await Promise.all([...sessions].map((id) => capture(id, "SessionEnd")))
    },
  }
}

// kemoryCommand is the kemory CLI on PATH, or else the kemory npm package
// through npx. KEMORY_ENV picks the Kemory environment, as for the CLI.
function kemoryCommand() {
  // A .cmd shim runs only through a shell, which npx gets on Windows.
  const name = process.platform === "win32" ? "kemory.exe" : "kemory"
  for (const dir of (process.env.PATH ?? "").split(delimiter)) {
    if (dir && existsSync(join(dir, name))) return [join(dir, name)]
  }
  return ["npx", "-y", "kemory"]
}
