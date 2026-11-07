package auth

import "testing"

func TestOnlyHTTPURLsReachTheBrowserOpener(t *testing.T) {
	for _, c := range []struct{ name, target string }{
		{"option flag", "-a Calculator"},
		{"help flag", "--help"},
		{"file url", "file:///etc/passwd"},
		{"javascript url", "javascript:alert(1)"},
		{"custom scheme", "vscode://evil"},
		{"empty host", "https://"},
		{"not a url", "not a url"},
	} {
		t.Run(c.name, func(t *testing.T) {
			if err := openBrowser(c.target); err == nil {
				t.Fatalf("%q was passed to the platform opener", c.target)
			}
		})
	}
}
