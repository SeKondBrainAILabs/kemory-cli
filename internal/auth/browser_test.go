package auth

import "testing"

func TestOnlyHTTPURLsReachTheBrowserOpener(t *testing.T) {
	for _, target := range []string{
		"-a Calculator",
		"--help",
		"file:///etc/passwd",
		"javascript:alert(1)",
		"vscode://evil",
		"https://",
		"not a url",
	} {
		if err := openBrowser(target); err == nil {
			t.Fatalf("%q was passed to the platform opener", target)
		}
	}
}
