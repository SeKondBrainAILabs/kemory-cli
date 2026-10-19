// Package hook implements what a coding agent's hooks run: loading Kemory
// context when a session starts and saving the session when it ends. Every
// agent integration calls these through `kemory hook`, so recall, capture and
// redaction exist once.
//
// The behaviour matches the Kemory plugin's session-start.sh, capture.sh and
// redact.py, and capture shares the plugin's ~/.kemory/.captured state, so
// a session is never stored twice whichever of the two captured it.
package hook

import "regexp"

// secret matches credential-shaped text. A keyword alone is not enough:
// developer conversations say "token" and "secret" constantly, so a keyword
// only redacts when an assignment and a credential-length value follow.
// Same pattern as the plugin's redact.py; keep the two in step.
var secret = regexp.MustCompile(`(?i)(bearer\s+[\w\-\.]{8,}` +
	`|(?:api[_-]?key|api[_-]?token|access[_-]?token|auth[_-]?token|token` +
	`|secret|password|passwd|pwd)\s*[:=]\s*["']?[^\s"',;}]{6,}` +
	`|sk-[A-Za-z0-9]{16,}|gh[pousr]_[A-Za-z0-9]{20,}` +
	`|AKIA[0-9A-Z]{16}|xox[baprs]-[A-Za-z0-9-]{10,}` +
	`|eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}` +
	`|-----BEGIN[^-]+PRIVATE KEY-----)`)

// Redact replaces credential-shaped substrings with a placeholder.
func Redact(text string) string {
	return secret.ReplaceAllString(text, "[REDACTED]")
}
