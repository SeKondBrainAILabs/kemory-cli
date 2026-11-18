// Package hook implements what a coding agent's hooks run: loading Kemory
// context when a session starts and saving the session when it ends. Every
// agent integration calls these through `kemory hook`, so recall, capture and
// redaction exist once.
//
// The behaviour matches the Kemory plugin's session-start.sh, capture.sh and
// redact.py, and capture shares the plugin's ~/.kemory/.captured state, so
// a session is never stored twice whichever of the two captured it.
package hook

import (
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

// The patterns and the algorithm in Redact mirror redact.py in the
// kemory-plugin repository at commit d7f8e2da; keep the two in step. Python's
// re engine and Go's RE2 differ in a few places, handled like this:
//
//   - RE2 has no lookbehind. STRIPE_KEY's "no lowercase letter right before"
//     rule is a prefix group, and only the key's own group is redacted.
//   - RE2 rejects repeat counts above 1000, and a pattern that fails to
//     compile panics at package init, which would break every kemory command.
//     The plugin's private-key scan is capped at 16384 characters, so it is
//     written in code (redactKeyBlocks), counting characters, not bytes.
//   - Python's \s and \w are Unicode, Go's are ASCII. wsClass and wordClass
//     list the same characters as Python's, so a no-break or em space between
//     a keyword and its value does not let the value through.
//
// Three accepted differences in output remain:
//
//	(a) \b is ASCII in Go, so next to a non-ASCII letter the two disagree:
//	    Go redacts more where such a letter sits right before sk- or a URL
//	    scheme, and less where Python's case folding makes U+017F look like s.
//	(b) Python's (?i) also matches U+0130 and U+0131 for i; Go's does not.
//	(c) A character whose Unicode category differs between the Unicode tables
//	    of Go and of Python is classed differently.
const (
	// wsClass is the body of a character class equal to Python's \s.
	wsClass = `\t\n\v\f\r\x1c-\x1f\x85\p{Z}`
	// wordClass is the body of a character class equal to Python's \w.
	wordClass = `\p{L}\p{N}_`
)

// secret matches credential-shaped text. A keyword alone is not enough:
// developer conversations say "token" and "secret" constantly, so a keyword
// only redacts when an assignment and a credential-length value follow. The
// key may be quoted (JSON, YAML, a Python dict), and JSON-escaped text puts a
// backslash before each quote. Names that end in "key" match too:
// SECRET...KEY (up to 24 characters between, so AWS_SECRET_ACCESS_KEY),
// PRIVATE_KEY, ACCESS_KEY and ACCOUNT_KEY. `secret` alone still needs the
// assignment, so "the secret key is stored in vault" is prose.
var secret = regexp.MustCompile(`(?i)(bearer[` + wsClass + `]+[` + wordClass + `\-.]{8,}` +
	`|(?:api[_-]?key|api[_-]?token|access[_-]?token|auth[_-]?token|token` +
	`|secret[a-z0-9_-]{0,24}?key|private[_-]?key|access[_-]?key|account[_-]?key` +
	`|secret|password|passwd|pwd)\\?["']?[` + wsClass + `]*[:=][` + wsClass + `]*\\?["']?[^` + wsClass + `"',;}]{6,}` +
	`|sk-[A-Za-z0-9]{16,}|gh[pousr]_[A-Za-z0-9]{20,}` +
	`|AKIA[0-9A-Z]{16}|xox[baprs]-[A-Za-z0-9-]{10,}` +
	`|eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,})`)

// A private key is secret in its body, not its header, so redactKeyBlocks takes
// the whole block, BEGIN line to END line (PEM, OpenSSH and PGP), as its own
// pass before secret: inside secret, a keyword such as `password=` starts
// earlier than the BEGIN line, wins the leftmost match and eats only
// "-----BEGIN".
var (
	keyBegin = regexp.MustCompile(`(?i)-----BEGIN [A-Z0-9 ]*PRIVATE KEY(?: BLOCK)?-----`)
	// keyEndStart finds where an END line could start; keyEnd then checks it.
	// The split keeps the END search inside the 16 KB window.
	keyEndStart = regexp.MustCompile(`(?i)-----END `)
	keyEnd      = regexp.MustCompile(`(?i)^-----END [A-Z0-9 ]*PRIVATE KEY(?: BLOCK)?-----`)
	// keyTail is what follows a BEGIN line with no END line (a truncated
	// paste): base64 runs and PEM/PGP header lines, separated by whitespace or
	// a literal backslash-n from a JSON-escaped key. The header names are
	// listed on purpose: a general `Name:` would eat ordinary lines.
	keyTail = regexp.MustCompile(`(?i)^(?:(?:[` + wsClass + `]|\\[rn])+(?:[A-Za-z0-9+/=]{16,}` +
		`|(?:Proc-Type|DEK-Info|Version|Comment|Hash|Charset):[^\n]*))*`)
)

// keyScanRunes caps the scan for an END line per BEGIN line (a 4096-bit RSA
// key is about 3.3 KB), so a text full of unterminated headers stays cheap.
const keyScanRunes = 16384

// Anthropic and OpenAI project and service keys have dashes and underscores
// after sk-, where secret's sk- branch stops. It is its own pattern and Redact
// takes the union of both patterns' matches: as a branch inside secret it won
// the leftmost match and swallowed a following password=/token=, JWT or PEM
// header, leaking the value. The \b keeps words like task-... and disk-...
// from matching.
var skDashed = regexp.MustCompile(`(?i)\bsk-[A-Za-z0-9_-]{16,}`)

// Vendor key shapes with a fixed prefix. Each is its own pattern, unioned in
// Redact, and not a branch of secret, for the reason given at skDashed. All
// four are case-sensitive: the vendors issue these prefixes in this case only,
// so matching any case catches no real key and would redact long glued
// uppercase constants such as RK_LIVE_ followed by 20 letters.
var (
	// A GitHub fine-grained personal access token. The two length rules keep
	// GITHUB_PAT_... names and github_pat_expiry_warning_threshold out.
	githubPAT = regexp.MustCompile(`github_pat_[A-Za-z0-9]{16,}_[A-Za-z0-9]{32,}`)

	// A Stripe secret or restricted key, live or test. No lowercase letter may
	// sit right before it, so risk_test_... and task_test_... identifiers stay.
	// The key still matches after a JSON escape (\n, \r, \t, \uXXXX) or a
	// percent-escape (%3D, %3d), whose last character can be a lowercase
	// letter. Only group 1, the key, is redacted.
	stripeKey = regexp.MustCompile(`(?:^|[^a-z]|\\[nrt]|\\u[0-9A-Fa-f]{4}|%[0-9A-Fa-f]{2})` +
		`([sr]k_(?:live|test)_[A-Za-z0-9]{20,})`)

	// A Google API key: AIza and 35 more characters.
	googleAPIKey = regexp.MustCompile(`AIza[0-9A-Za-z_-]{35}`)

	// A GitLab personal access token. Other GitLab token prefixes are not
	// covered.
	gitlabPAT = regexp.MustCompile(`glpat-[A-Za-z0-9_.-]{20,}`)
)

// urlPassword matches the password in scheme://user:password@host. Only group
// 1 is redacted, so the user and host stay readable. The user runs to the
// first ':' and may contain '@' (user@server:pass@host). The greedy password
// class ends at the last @ before a '/', '?' or '#', so a password containing
// '@' is fully redacted. The scheme is capped at 32 characters: uncapped, the
// scan ran to the end of the text from every word start.
var urlPassword = regexp.MustCompile(`(?i)\b[a-z][a-z0-9+.-]{0,31}://[^` + wsClass + `:/]{0,256}:` +
	`([^` + wsClass + `/?#]{1,256})@`)

const placeholder = "[REDACTED]"

// Redact replaces credential-shaped substrings with a placeholder.
func Redact(text string) string {
	// Private-key blocks go first, as their own pass. Then the union of the
	// matches of secret, skDashed and the four vendor key patterns, and
	// urlPassword's password group. Overlapping spans merge into one
	// placeholder.
	text = redactKeyBlocks(text)
	var spans [][2]int
	for _, p := range []*regexp.Regexp{secret, skDashed, githubPAT, googleAPIKey, gitlabPAT} {
		for _, m := range p.FindAllStringIndex(text, -1) {
			spans = append(spans, [2]int{m[0], m[1]})
		}
	}
	for _, p := range []*regexp.Regexp{stripeKey, urlPassword} {
		for _, m := range p.FindAllStringSubmatchIndex(text, -1) {
			spans = append(spans, [2]int{m[2], m[3]})
		}
	}
	sort.Slice(spans, func(i, j int) bool {
		if spans[i][0] != spans[j][0] {
			return spans[i][0] < spans[j][0]
		}
		return spans[i][1] < spans[j][1]
	})
	var merged [][2]int
	for _, s := range spans {
		if n := len(merged); n > 0 && s[0] < merged[n-1][1] {
			merged[n-1][1] = max(merged[n-1][1], s[1])
		} else {
			merged = append(merged, s)
		}
	}
	var out strings.Builder
	pos := 0
	for _, s := range merged {
		out.WriteString(text[pos:s[0]])
		out.WriteString(placeholder)
		pos = s[1]
	}
	out.WriteString(text[pos:])
	return out.String()
}

// redactKeyBlocks replaces each private-key block with the placeholder. A
// block runs from its BEGIN line to the first END line that starts within
// keyScanRunes characters of the BEGIN line's end. Without one it is the BEGIN
// line plus the base64 and header lines that follow. Scanning resumes after
// each replaced block, so a BEGIN line inside one is not looked at again.
func redactKeyBlocks(text string) string {
	var out strings.Builder
	cursor := 0
	for {
		loc := keyBegin.FindStringIndex(text[cursor:])
		if loc == nil {
			break
		}
		start, beginEnd := cursor+loc[0], cursor+loc[1]
		end := keyBlockEnd(text, beginEnd)
		out.WriteString(text[cursor:start])
		out.WriteString(placeholder)
		cursor = end
	}
	if cursor == 0 {
		return text
	}
	out.WriteString(text[cursor:])
	return out.String()
}

// keyBlockEnd returns where the block whose BEGIN line ends at beginEnd ends.
func keyBlockEnd(text string, beginEnd int) int {
	limit := beginEnd
	for n := 0; n < keyScanRunes && limit < len(text); n++ {
		_, size := utf8.DecodeRuneInString(text[limit:])
		limit += size
	}
	// An END line may start at the limit and end past it.
	window := text[beginEnd:min(len(text), limit+len("-----END "))]
	for _, c := range keyEndStart.FindAllStringIndex(window, -1) {
		start := beginEnd + c[0]
		if start > limit {
			break
		}
		if m := keyEnd.FindStringIndex(text[start:]); m != nil {
			return start + m[1]
		}
	}
	return beginEnd + keyTail.FindStringIndex(text[beginEnd:])[1]
}
