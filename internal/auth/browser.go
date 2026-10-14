package auth

import (
	"context"
	"fmt"
	"net/url"
	"os/exec"
	"runtime"
)

// openBrowser opens target in the user's default browser. The URL comes from
// the issuer's response, so only an absolute http(s) URL is accepted: anything
// else (a leading "-" read as an option, a file: or custom-scheme URL) is
// refused before it reaches the platform opener.
func openBrowser(target string) error {
	u, err := url.Parse(target)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return fmt.Errorf("refusing to open %q: not an http(s) URL", target)
	}
	safe := u.String()
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.CommandContext(context.Background(), "open", safe) //nolint:gosec // safe is a validated http(s) URL, passed as one argument with no shell
	case "windows":
		cmd = exec.CommandContext(context.Background(), "rundll32", "url.dll,FileProtocolHandler", safe) //nolint:gosec // as above
	default:
		cmd = exec.CommandContext(context.Background(), "xdg-open", safe) //nolint:gosec // as above
	}
	return cmd.Start()
}
