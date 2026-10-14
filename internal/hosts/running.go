package hosts

import (
	"context"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// Claude Desktop keeps its config in memory and writes the whole file back
// whenever its own settings change, which happens every few minutes while it
// runs. An entry added while it is open is gone at the next write, so the
// entry has to be written while the app is closed.

// RewritesWhileRunning reports whether host is an app that overwrites its
// config file from memory while it runs, so an edit only lasts if the app is
// closed. Only Claude Desktop does.
func RewritesWhileRunning(name string) bool { return name == "claude-desktop" }

// RunningCheck reports whether the host app is running now. A variable so
// tests do not depend on the machine's process list.
var RunningCheck = isRunning

func isRunning(name string) bool {
	if !RewritesWhileRunning(name) {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	switch runtime.GOOS {
	case "darwin":
		out, err := exec.CommandContext(ctx, "ps", "-axo", "comm=").Output()
		if err != nil {
			return false
		}
		for _, line := range strings.Split(string(out), "\n") {
			if strings.HasSuffix(strings.TrimSpace(line), "/Claude.app/Contents/MacOS/Claude") {
				return true
			}
		}
	case "windows":
		out, err := exec.CommandContext(ctx, "tasklist", "/FI", "IMAGENAME eq Claude.exe", "/NH").Output()
		if err != nil {
			return false
		}
		return strings.Contains(strings.ToLower(string(out)), "claude.exe")
	}
	return false
}
