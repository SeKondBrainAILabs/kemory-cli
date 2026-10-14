// Package jsonedit edits one key of a JSON object while leaving the rest of
// the document as the user wrote it: key order, string escaping and the
// values of every other key are preserved.
//
// MCP host configs (~/.claude.json, claude_desktop_config.json, ...) belong to
// the user and to the host app. Round-tripping them through a Go map would
// reorder their keys and HTML-escape '&', '<' and '>' in their strings.
package jsonedit

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// Object is a JSON object that remembers its key order.
type Object struct {
	keys   []string
	values map[string]json.RawMessage
}

// NewObject returns an empty object.
func NewObject() *Object {
	return &Object{values: map[string]json.RawMessage{}}
}

// Parse reads a JSON object. Empty or whitespace-only input is an empty
// object, matching how the hosts treat an empty config file.
func Parse(data []byte) (*Object, error) {
	o := NewObject()
	if len(bytes.TrimSpace(data)) == 0 {
		return o, nil
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '{' {
		return nil, errors.New("not a JSON object")
	}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, ok := tok.(string)
		if !ok {
			return nil, fmt.Errorf("unexpected token %v", tok)
		}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return nil, err
		}
		o.Set(key, raw)
	}
	if _, err := dec.Token(); err != nil { // closing brace
		return nil, err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("trailing data after the JSON object")
	}
	return o, nil
}

// Keys returns the keys in document order.
func (o *Object) Keys() []string { return append([]string(nil), o.keys...) }

// Get returns the raw value of key.
func (o *Object) Get(key string) (json.RawMessage, bool) {
	v, ok := o.values[key]
	return v, ok
}

// Set replaces key's value in place, or appends it when new.
func (o *Object) Set(key string, value json.RawMessage) {
	if _, ok := o.values[key]; !ok {
		o.keys = append(o.keys, key)
	}
	o.values[key] = value
}

// SetValue marshals v without HTML escaping and sets it.
func (o *Object) SetValue(key string, v any) error {
	raw, err := marshal(v)
	if err != nil {
		return err
	}
	o.Set(key, raw)
	return nil
}

// Delete removes key.
func (o *Object) Delete(key string) {
	if _, ok := o.values[key]; !ok {
		return
	}
	delete(o.values, key)
	for i, k := range o.keys {
		if k == key {
			o.keys = append(o.keys[:i], o.keys[i+1:]...)
			return
		}
	}
}

// Len is the number of keys.
func (o *Object) Len() int { return len(o.keys) }

// Object returns key's value as an Object: an empty one when key is absent or
// null, an error when it is something other than an object.
func (o *Object) Object(key string) (*Object, error) {
	raw, ok := o.values[key]
	if !ok || string(bytes.TrimSpace(raw)) == "null" {
		return NewObject(), nil
	}
	child, err := Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("%q is not a JSON object: %w", key, err)
	}
	return child, nil
}

// SetObject stores child under key.
func (o *Object) SetObject(key string, child *Object) error {
	raw, err := child.Marshal("")
	if err != nil {
		return err
	}
	o.Set(key, raw)
	return nil
}

// Marshal renders the object with two-space indentation, starting at the
// given line prefix.
func (o *Object) Marshal(prefix string) ([]byte, error) {
	if len(o.keys) == 0 {
		return []byte("{}"), nil
	}
	var b bytes.Buffer
	b.WriteString("{\n")
	for i, k := range o.keys {
		key, err := marshal(k)
		if err != nil {
			return nil, err
		}
		var val bytes.Buffer
		if err := json.Indent(&val, o.values[k], prefix+"  ", "  "); err != nil {
			return nil, fmt.Errorf("value of %q: %w", k, err)
		}
		b.WriteString(prefix + "  ")
		b.Write(key)
		b.WriteString(": ")
		b.Write(val.Bytes())
		if i < len(o.keys)-1 {
			b.WriteString(",")
		}
		b.WriteString("\n")
	}
	b.WriteString(prefix + "}")
	return b.Bytes(), nil
}

// marshal encodes v without escaping '&', '<' and '>'.
func marshal(v any) (json.RawMessage, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return json.RawMessage(strings.TrimRight(b.String(), "\n")), nil
}
