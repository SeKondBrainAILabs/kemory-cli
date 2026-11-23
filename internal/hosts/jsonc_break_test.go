package hosts

import (
	"strings"
	"testing"
)

// Removing two entries that end the servers object, as doctor and connect do
// with duplicate kemory entries, must leave valid settings with the other
// server kept, however the object is laid out.
func TestZedRemovesTwoTrailingEntries(t *testing.T) {
	for name, settings := range map[string]string{
		"one per line": `{
  "theme": "One Dark",
  "context_servers": {
    "other": { "command": "other" },
    "kemory": { "command": "kemory" },
    "kemory-2": { "command": "kemory" }
  }
}
`,
		"on one line": `{
  "theme": "One Dark",
  "context_servers": { "other": { "command": "other" }, "kemory": { "command": "kemory" }, "kemory-2": { "command": "kemory" }}
}
`,
	} {
		t.Run(name, func(t *testing.T) {
			setHome(t)
			path := ResolveConfig("zed")
			writeTestFile(t, path, settings)
			if err := RemoveEntries("zed", path, []string{"kemory", "kemory-2"}); err != nil {
				t.Fatalf("remove: %v\n%s", err, read(t, path))
			}
			left := read(t, path)
			if strings.Contains(left, "kemory") || !strings.Contains(left, `"other"`) || !strings.Contains(left, "One Dark") {
				t.Fatalf("after remove:\n%s", left)
			}
		})
	}
}
