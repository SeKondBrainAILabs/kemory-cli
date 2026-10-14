package hosts

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"reflect"
	"strings"

	"github.com/BurntSushi/toml"
)

// Codex and Grok keep servers as [mcp_servers.<name>] tables in a TOML file
// that also holds the user's own settings and comments. A TOML library cannot
// write a file back with its comments, so the kemory table is edited as text:
// its old tables are cut out and a new one is appended. Decoding the file
// before and after proves that nothing but the kemory entry changed.

const tomlServersKey = "mcp_servers"

func tomlServerNames(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	doc := map[string]any{}
	if _, err := toml.Decode(string(data), &doc); err != nil {
		return nil
	}
	servers, _ := doc[tomlServersKey].(map[string]any)
	names := make([]string, 0, len(servers))
	for n := range servers {
		names = append(names, n)
	}
	return sorted(names)
}

func readTOML(path string) (string, map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", nil, err
	}
	doc := map[string]any{}
	if _, err := toml.Decode(string(data), &doc); err != nil {
		return "", nil, invalid(path, "is not valid TOML")
	}
	return string(data), doc, nil
}

func writeTOMLEntry(path, name string, launch Launch) error {
	text, before, err := readTOML(path)
	if err != nil {
		return err
	}
	block := tomlBlock(name, launch)
	text = strings.TrimRight(cutTables(text, name), "\n")
	if strings.TrimSpace(text) != "" {
		text += "\n\n"
	} else {
		text = ""
	}
	text += block

	after := map[string]any{}
	if _, err := toml.Decode(text, &after); err != nil {
		return invalid(path, fmt.Sprintf("defines %s in a form kemory cannot edit. Set the %s entry by hand to:\n\n%s", tomlServersKey, name, block))
	}
	want := map[string]any{"command": launch.Command, "args": toAny(launch.Args)}
	got, _ := after[tomlServersKey].(map[string]any)
	if !reflect.DeepEqual(got[name], want) || !reflect.DeepEqual(without(before, name), without(after, name)) {
		return invalid(path, fmt.Sprintf("could not be edited safely. Set the %s entry by hand to:\n\n%s", name, block))
	}
	return writeFile(path, []byte(text))
}

// removeTOMLEntries returns path when the file would be left empty, so the
// caller deletes it instead.
func removeTOMLEntries(path string, names []string) (string, error) {
	if _, err := os.Stat(path); err != nil {
		return "", err
	}
	text, before, err := readTOML(path)
	if err != nil {
		return "", err
	}
	for _, n := range names {
		text = cutTables(text, n)
	}
	after := map[string]any{}
	if _, err := toml.Decode(text, &after); err != nil {
		return "", invalid(path, "could not be edited safely")
	}
	expect := before
	for _, n := range names {
		expect = without(expect, n)
	}
	if !reflect.DeepEqual(expect, without(after, "")) {
		return "", invalid(path, "defines a kemory server in a form kemory cannot remove. Remove it by hand")
	}
	if strings.TrimSpace(text) == "" {
		return path, nil
	}
	return "", writeFile(path, []byte(text))
}

// without returns doc minus mcp_servers.<name>, dropping mcp_servers when that
// leaves it empty. An empty name only normalises.
func without(doc map[string]any, name string) map[string]any {
	out := make(map[string]any, len(doc))
	for k, v := range doc {
		out[k] = v
	}
	servers, ok := out[tomlServersKey].(map[string]any)
	if !ok {
		return out
	}
	rest := make(map[string]any, len(servers))
	for k, v := range servers {
		if k != name {
			rest[k] = v
		}
	}
	if len(rest) == 0 {
		delete(out, tomlServersKey)
	} else {
		out[tomlServersKey] = rest
	}
	return out
}

func toAny(ss []string) []any {
	out := make([]any, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}

func tomlBlock(name string, launch Launch) string {
	args := make([]string, len(launch.Args))
	for i, a := range launch.Args {
		args[i] = tomlString(a)
	}
	return fmt.Sprintf("[%s.%s]\ncommand = %s\nargs = [%s]\n",
		tomlServersKey, tomlKey(name), tomlString(launch.Command), strings.Join(args, ", "))
}

// tomlKey writes name bare when TOML allows it, else quoted.
func tomlKey(name string) string {
	if name == "" {
		return `""`
	}
	for _, r := range name {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '-' && r != '_' {
			return tomlString(name)
		}
	}
	return name
}

// tomlString writes s as a TOML basic string.
func tomlString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 || r == 0x7f {
				fmt.Fprintf(&b, `\u%04X`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

// cutTables removes the [mcp_servers.<name>] table and its sub-tables. Each
// runs from its header to the next header or the end of the file, less the
// comments and blank lines right before that point, which are not its own.
func cutTables(text, name string) string {
	headers := tableHeaders(text)
	var b strings.Builder
	pos := 0
	for i, h := range headers {
		if !isEntryTable(h.path, name) {
			continue
		}
		next := len(text)
		if i+1 < len(headers) {
			next = headers[i+1].start
		}
		end := keepLeadingComments(text, h.end, next)
		b.WriteString(text[pos:h.start])
		pos = end
		// The blank line that separated the cut table from what preceded
		// it now separates nothing.
		if out := b.String(); out == "" || strings.HasSuffix(out, "\n\n") {
			pos = skipBlankLines(text, pos, next)
		}
	}
	b.WriteString(text[pos:])
	return b.String()
}

// skipBlankLines moves i past whitespace-only lines, never past limit.
func skipBlankLines(text string, i, limit int) int {
	for i < limit {
		nl := strings.IndexByte(text[i:limit], '\n')
		if nl < 0 || strings.TrimSpace(text[i:i+nl]) != "" {
			return i
		}
		i += nl + 1
	}
	return i
}

func isEntryTable(path []string, name string) bool {
	return len(path) >= 2 && path[0] == tomlServersKey && path[1] == name
}

// keepLeadingComments moves end back over the comment and blank lines that
// directly precede it, never past floor.
func keepLeadingComments(text string, floor, end int) int {
	for end > floor {
		prev := strings.LastIndexByte(text[:end-1], '\n') + 1
		if prev < floor {
			break
		}
		line := strings.TrimSpace(text[prev:end])
		if line != "" && !strings.HasPrefix(line, "#") {
			break
		}
		end = prev
	}
	return end
}

type tableHeader struct {
	start, end int // the header line, end excluding its newline
	path       []string
}

// tableHeaders finds the table headers in a TOML document. It tracks strings,
// comments and open brackets, so a '[' inside a value is never taken for one.
func tableHeaders(text string) []tableHeader {
	var headers []tableHeader
	depth := 0
	for i := 0; i < len(text); {
		lineEnd := strings.IndexByte(text[i:], '\n')
		if lineEnd < 0 {
			lineEnd = len(text)
		} else {
			lineEnd += i
		}
		trimmed := strings.TrimLeft(text[i:lineEnd], " \t")
		if depth == 0 && strings.HasPrefix(trimmed, "[") {
			if path, ok := parseHeader(trimmed); ok {
				headers = append(headers, tableHeader{start: i, end: lineEnd, path: path})
				i = lineEnd + 1
				continue
			}
		}
		i = scanValue(text, i, &depth)
	}
	return headers
}

// scanValue reads from i to the end of the line it is on, or past the end of
// a multi-line string that starts on it, tracking bracket depth. It returns
// the index just past the newline that ends the read.
func scanValue(text string, i int, depth *int) int {
	for i < len(text) {
		c := text[i]
		switch {
		case c == '\n':
			return i + 1
		case c == '#':
			nl := strings.IndexByte(text[i:], '\n')
			if nl < 0 {
				return len(text)
			}
			return i + nl + 1
		case strings.HasPrefix(text[i:], `"""`):
			i = skipMultiline(text, i+3, `"""`, true)
		case strings.HasPrefix(text[i:], `'''`):
			i = skipMultiline(text, i+3, `'''`, false)
		case c == '"':
			i = skipString(text, i+1, '"', true)
		case c == '\'':
			i = skipString(text, i+1, '\'', false)
		case c == '[' || c == '{':
			*depth++
			i++
		case c == ']' || c == '}':
			if *depth > 0 {
				*depth--
			}
			i++
		default:
			i++
		}
	}
	return i
}

func skipString(text string, i int, quote byte, escapes bool) int {
	for i < len(text) {
		switch text[i] {
		case '\\':
			if escapes {
				i += 2
				continue
			}
		case quote:
			return i + 1
		case '\n':
			return i // unterminated; decoding has already rejected such a file
		}
		i++
	}
	return i
}

func skipMultiline(text string, i int, delim string, escapes bool) int {
	for i < len(text) {
		if escapes && text[i] == '\\' {
			i += 2
			continue
		}
		if strings.HasPrefix(text[i:], delim) {
			i += len(delim)
			// Up to two more quotes belong to the string: the delimiter is
			// the last three of the run.
			for n := 0; n < 2 && i < len(text) && text[i] == delim[0]; n++ {
				i++
			}
			return i
		}
		i++
	}
	return i
}

// parseHeader reads a [table] or [[array]] header line into its key path.
func parseHeader(line string) ([]string, bool) {
	open, closing := "[", "]"
	if strings.HasPrefix(line, "[[") {
		open, closing = "[[", "]]"
	}
	rest := line[len(open):]
	var path []string
	for {
		rest = strings.TrimLeft(rest, " \t")
		key, n, ok := parseKey(rest)
		if !ok {
			return nil, false
		}
		path = append(path, key)
		rest = strings.TrimLeft(rest[n:], " \t")
		switch {
		case strings.HasPrefix(rest, "."):
			rest = rest[1:]
		case strings.HasPrefix(rest, closing):
			tail := strings.TrimSpace(rest[len(closing):])
			return path, tail == "" || strings.HasPrefix(tail, "#")
		default:
			return nil, false
		}
	}
}

// parseKey reads one bare or quoted key and returns it with its length.
func parseKey(s string) (string, int, bool) {
	if s == "" {
		return "", 0, false
	}
	switch s[0] {
	case '"':
		end := skipString(s, 1, '"', true)
		if end > len(s) || end < 2 || s[end-1] != '"' {
			return "", 0, false
		}
		var doc map[string]any
		if _, err := toml.Decode("k = "+s[:end], &doc); err != nil {
			return "", 0, false
		}
		key, ok := doc["k"].(string)
		return key, end, ok
	case '\'':
		end := strings.IndexByte(s[1:], '\'')
		if end < 0 {
			return "", 0, false
		}
		return s[1 : end+1], end + 2, true
	}
	n := 0
	for n < len(s) {
		c := s[n]
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '-' && c != '_' {
			break
		}
		n++
	}
	return s[:n], n, n > 0
}
