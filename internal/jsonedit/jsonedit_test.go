package jsonedit

import (
	"strings"
	"testing"
)

const userConfig = `{
  "zeta": 1,
  "alpha": {"cmd": "a && b <c>", "nested": [1, 2]},
  "mcpServers": {"other": {"command": "x"}},
  "beta": "ünïcode"
}`

func TestEditingOneKeyPreservesTheRest(t *testing.T) {
	o, err := Parse([]byte(userConfig))
	if err != nil {
		t.Fatal(err)
	}
	servers, err := o.Object("mcpServers")
	if err != nil {
		t.Fatal(err)
	}
	if err := servers.SetValue("kemory", map[string]any{"command": "kemory", "args": []string{"mcp", "serve"}}); err != nil {
		t.Fatal(err)
	}
	if err := o.SetObject("mcpServers", servers); err != nil {
		t.Fatal(err)
	}
	out, err := o.Marshal("")
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, want := range []string{`"cmd": "a && b <c>"`, `"beta": "ünïcode"`, `"other": {`, `"kemory": {`} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %s in:\n%s", want, s)
		}
	}
	if strings.Index(s, `"zeta"`) > strings.Index(s, `"alpha"`) || strings.Index(s, `"other"`) > strings.Index(s, `"kemory"`) {
		t.Fatalf("key order changed:\n%s", s)
	}
	back, err := Parse(out)
	if err != nil || strings.Join(back.Keys(), ",") != "zeta,alpha,mcpServers,beta" {
		t.Fatalf("round trip: %v %v", back, err)
	}
}

func TestEmptyInputIsAnEmptyObject(t *testing.T) {
	for _, in := range []string{"", "  \n", "{}"} {
		o, err := Parse([]byte(in))
		if err != nil || o.Len() != 0 {
			t.Fatalf("%q: %v %v", in, o, err)
		}
	}
}

func TestInvalidDocumentsAreRejected(t *testing.T) {
	for _, in := range []string{"[1]", "{ not json ]", `{"a": 1} {"b": 2}`, `"text"`} {
		if _, err := Parse([]byte(in)); err == nil {
			t.Fatalf("%q was accepted", in)
		}
	}
}

func TestNonObjectChildIsAnError(t *testing.T) {
	o, _ := Parse([]byte(`{"mcpServers": [1], "n": null}`))
	if _, err := o.Object("mcpServers"); err == nil {
		t.Fatal("an array mcpServers was accepted")
	}
	if c, err := o.Object("n"); err != nil || c.Len() != 0 {
		t.Fatal("null should read as an empty object")
	}
}

func TestDelete(t *testing.T) {
	o, _ := Parse([]byte(`{"a": 1, "b": 2, "c": 3}`))
	o.Delete("b")
	o.Delete("missing")
	if strings.Join(o.Keys(), ",") != "a,c" {
		t.Fatalf("keys: %v", o.Keys())
	}
}
