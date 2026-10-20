package hosts

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"reflect"
	"sort"
	"strings"
)

// Zed keeps its settings in JSON with comments and trailing commas, written
// by hand. A JSON library cannot write such a file back with its comments, so
// the kemory entry is spliced into the text, as for TOML: a small scanner that
// skips comments and strings finds the entry, or the servers object, and
// decoding the file before and after proves that nothing but the kemory entry
// changed.

// jsoncMember is one member of an object, by position in the text.
type jsoncMember struct {
	name                 string
	start                int // the key's opening quote
	valueStart, valueEnd int
	end                  int // past the comma after the value, or valueEnd
}

type jsoncScanner struct {
	s       string
	i       int
	comment bool // a comment was seen
}

var errJSONC = errors.New("not valid JSON")

// skip passes whitespace and comments.
func (p *jsoncScanner) skip() {
	for p.i < len(p.s) {
		rest := p.s[p.i:]
		switch {
		case rest[0] == ' ' || rest[0] == '\t' || rest[0] == '\n' || rest[0] == '\r':
			p.i++
		case strings.HasPrefix(rest, "//"):
			p.comment = true
			if j := strings.IndexByte(rest, '\n'); j >= 0 {
				p.i += j + 1
			} else {
				p.i = len(p.s)
			}
		case strings.HasPrefix(rest, "/*"):
			p.comment = true
			if j := strings.Index(rest[2:], "*/"); j >= 0 {
				p.i += j + 4
			} else {
				p.i = len(p.s)
			}
		default:
			return
		}
	}
}

// str reads the string at the current position.
func (p *jsoncScanner) str() (string, error) {
	if p.i >= len(p.s) || p.s[p.i] != '"' {
		return "", errJSONC
	}
	start := p.i
	for p.i++; p.i < len(p.s); p.i++ {
		switch p.s[p.i] {
		case '\\':
			p.i++
		case '"':
			p.i++
			var out string
			if err := json.Unmarshal([]byte(p.s[start:p.i]), &out); err != nil {
				return "", errJSONC
			}
			return out, nil
		}
	}
	return "", errJSONC
}

// value passes the value at the current position.
func (p *jsoncScanner) value() error {
	if p.i >= len(p.s) {
		return errJSONC
	}
	switch p.s[p.i] {
	case '"':
		_, err := p.str()
		return err
	case '{':
		_, _, err := p.members()
		return err
	case '[':
		for p.i++; ; {
			p.skip()
			switch {
			case p.i >= len(p.s):
				return errJSONC
			case p.s[p.i] == ']':
				p.i++
				return nil
			case p.s[p.i] == ',':
				p.i++
			default:
				if err := p.value(); err != nil {
					return err
				}
			}
		}
	}
	start := p.i
	for p.i < len(p.s) && !strings.ContainsRune(",}] \t\r\n/", rune(p.s[p.i])) {
		p.i++
	}
	if p.i == start {
		return errJSONC
	}
	return nil
}

// members reads the object at the current position: its members, and the
// position of its opening brace.
func (p *jsoncScanner) members() (members []jsoncMember, open int, err error) {
	if p.i >= len(p.s) || p.s[p.i] != '{' {
		return nil, 0, errJSONC
	}
	open = p.i
	for p.i++; ; {
		p.skip()
		switch {
		case p.i >= len(p.s):
			return nil, 0, errJSONC
		case p.s[p.i] == '}':
			p.i++
			return members, open, nil
		case p.s[p.i] == ',':
			p.i++
			continue
		}
		m := jsoncMember{start: p.i}
		if m.name, err = p.str(); err != nil {
			return nil, 0, err
		}
		p.skip()
		if p.i >= len(p.s) || p.s[p.i] != ':' {
			return nil, 0, errJSONC
		}
		p.i++
		p.skip()
		m.valueStart = p.i
		if err := p.value(); err != nil {
			return nil, 0, err
		}
		m.valueEnd, m.end = p.i, p.i
		p.skip()
		if p.i < len(p.s) && p.s[p.i] == ',' {
			p.i++
			m.end = p.i
		}
		members = append(members, m)
	}
}

// stripJSONC is text as plain JSON: without comments or trailing commas.
func stripJSONC(text string) string {
	plain, _ := stripComments(text)
	return plain
}

// stripComments is stripJSONC, and whether text had a comment.
func stripComments(text string) (string, bool) {
	var b strings.Builder
	p := jsoncScanner{s: text}
	for p.i < len(text) {
		c := text[p.i]
		switch {
		case c == '"':
			start := p.i
			if _, err := p.str(); err != nil {
				b.WriteString(text[start:])
				return b.String(), p.comment
			}
			b.WriteString(text[start:p.i])
		case strings.HasPrefix(text[p.i:], "//") || strings.HasPrefix(text[p.i:], "/*"):
			p.skip()
			b.WriteByte(' ')
		case c == ',':
			p.i++
			save := p.i
			p.skip()
			if p.i < len(text) && (text[p.i] == '}' || text[p.i] == ']') {
				continue
			}
			p.i = save
			b.WriteByte(',')
		default:
			b.WriteByte(c)
			p.i++
		}
	}
	return b.String(), p.comment
}

// decodeJSONC decodes text, comments and trailing commas allowed. Blank text
// is an empty object.
func decodeJSONC(text string) (map[string]any, error) {
	doc := map[string]any{}
	if strings.TrimSpace(stripJSONC(text)) == "" {
		return doc, nil
	}
	if err := json.Unmarshal([]byte(stripJSONC(text)), &doc); err != nil {
		return nil, errJSONC
	}
	return doc, nil
}

// readJSONC reads the file at path, missing being empty, and its root object.
func readJSONC(path string) (string, map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", nil, err
	}
	doc, err := decodeJSONC(string(data))
	if err != nil {
		return "", nil, invalid(path, "is not valid JSON")
	}
	return string(data), doc, nil
}

// jsoncServers returns the servers object under key, or nil.
func jsoncServers(doc map[string]any, key string) map[string]any {
	servers, _ := doc[key].(map[string]any)
	return servers
}

func jsoncServerNames(path, key string) []string {
	_, doc, err := readJSONC(path)
	if err != nil {
		return nil
	}
	var names []string
	for n := range jsoncServers(doc, key) {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// jsoncObject finds the root object of text, and the member key in it.
func jsoncObject(text, key string) (root []jsoncMember, open int, member *jsoncMember, err error) {
	p := jsoncScanner{s: text}
	p.skip()
	root, open, err = p.members()
	if err != nil {
		return nil, 0, nil, err
	}
	for i := range root {
		if root[i].name == key {
			return root, open, &root[i], nil
		}
	}
	return root, open, nil, nil
}

func writeJSONCEntry(path, key, name string, entry any) error {
	text, before, err := readJSONC(path)
	if err != nil {
		return err
	}
	body, err := json.MarshalIndent(entry, "    ", "  ")
	if err != nil {
		return err
	}
	quoted, _ := json.Marshal(name)
	quotedKey, _ := json.Marshal(key)
	line := "\n    " + string(quoted) + ": " + string(body)

	if strings.TrimSpace(stripJSONC(text)) == "" {
		text = "{\n  " + string(quotedKey) + ": {" + line + "\n  }\n}\n"
	} else {
		root, open, servers, err := jsoncObject(text, key)
		if err != nil {
			return invalid(path, "is not valid JSON")
		}
		switch {
		case servers == nil:
			insert := "\n  " + string(quotedKey) + ": {" + line + "\n  }"
			if len(root) > 0 {
				insert += ","
			}
			text = text[:open+1] + insert + text[open+1:]
		case text[servers.valueStart] != '{':
			return invalid(path, "has a "+key+" that is not a JSON object")
		default:
			p := jsoncScanner{s: text, i: servers.valueStart}
			members, inner, err := p.members()
			if err != nil {
				return invalid(path, "is not valid JSON")
			}
			replaced := false
			for _, m := range members {
				if m.name == name {
					text = text[:m.valueStart] + string(body) + text[m.valueEnd:]
					replaced = true
					break
				}
			}
			if !replaced {
				if len(members) > 0 {
					line += ","
				}
				text = text[:inner+1] + line + text[inner+1:]
			}
		}
	}

	// Prove that only the entry changed.
	after, err := decodeJSONC(text)
	if err != nil {
		return fmt.Errorf("%s: kemory could not edit it safely", path)
	}
	want, err := withEntry(before, key, name, entry)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(after, want) {
		return fmt.Errorf("%s: kemory could not edit it safely", path)
	}
	return writeFile(path, []byte(text))
}

// withEntry is doc with entry set as key.name, as decoded JSON.
func withEntry(doc map[string]any, key, name string, entry any) (map[string]any, error) {
	raw, err := json.Marshal(entry)
	if err != nil {
		return nil, err
	}
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, err
	}
	out := map[string]any{}
	for k, v := range doc {
		out[k] = v
	}
	servers := map[string]any{}
	for k, v := range jsoncServers(doc, key) {
		servers[k] = v
	}
	servers[name] = decoded
	out[key] = servers
	return out, nil
}

// removeJSONCEntries returns path when nothing but the emptied servers object
// and no comment would remain, so the caller deletes the file instead.
func removeJSONCEntries(path, key string, names []string) (string, error) {
	if _, err := os.Stat(path); err != nil {
		return "", err
	}
	text, before, err := readJSONC(path)
	if err != nil {
		return "", err
	}
	_, _, servers, err := jsoncObject(text, key)
	if err != nil || servers == nil || text[servers.valueStart] != '{' {
		return "", err
	}
	p := jsoncScanner{s: text, i: servers.valueStart}
	members, _, err := p.members()
	if err != nil {
		return "", invalid(path, "is not valid JSON")
	}
	drop := map[string]bool{}
	for _, n := range names {
		drop[n] = true
	}
	// From the last member back, so earlier positions stay valid.
	for i := len(members) - 1; i >= 0; i-- {
		m := members[i]
		if !drop[m.name] {
			continue
		}
		start, end := m.start, m.end
		if end == m.valueEnd {
			// The last member: take the comma before it instead.
			j := start - 1
			for j >= 0 && strings.ContainsRune(" \t\r\n", rune(text[j])) {
				j--
			}
			if j >= 0 && text[j] == ',' {
				start = j
			}
		}
		// Take the line's indentation with it.
		for start > 0 && (text[start-1] == ' ' || text[start-1] == '\t') {
			start--
		}
		if start > 0 && text[start-1] == '\n' {
			start--
		}
		text = text[:start] + text[end:]
	}

	after, err := decodeJSONC(text)
	if err != nil {
		return "", fmt.Errorf("%s: kemory could not edit it safely", path)
	}
	want := map[string]any{}
	for k, v := range before {
		want[k] = v
	}
	left := map[string]any{}
	for k, v := range jsoncServers(before, key) {
		if !drop[k] {
			left[k] = v
		}
	}
	want[key] = left
	if !reflect.DeepEqual(after, want) {
		return "", fmt.Errorf("%s: kemory could not edit it safely", path)
	}
	if _, comment := stripComments(text); len(after) == 1 && len(left) == 0 && !comment {
		return path, nil
	}
	return "", writeFile(path, []byte(text))
}
