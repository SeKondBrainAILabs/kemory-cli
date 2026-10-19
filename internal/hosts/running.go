package hosts

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Claude Desktop keeps its config in memory and writes the whole file back
// whenever its own settings change, which happens every few minutes while it
// runs. An entry added while it is open is gone at the next write, so the
// entry has to be written while the app is closed.

// macOSApp is the main executable of Claude Desktop on macOS. The Claude Code
// build it bundles lives in a lowercase claude.app, which must not match.
const macOSApp = "/Claude.app/Contents/MacOS/Claude"

// RewritesWhileRunning reports whether host is an app that overwrites its
// config file from memory while it runs, so an edit only lasts if the app is
// closed. Only Claude Desktop does.
func RewritesWhileRunning(name string) bool { return name == "claude-desktop" }

// Variables so tests do not depend on, or act on, the machine's apps.
var (
	// RunningCheck reports whether the host app is running now.
	RunningCheck = isRunning
	// InsideApp reports whether this process runs inside the host app, as it
	// does in Claude Desktop's own terminal, and so would stop with it.
	InsideApp = insideApp
	// QuitApp asks the host app to quit and waits until it has.
	QuitApp = quitApp
	// OpenApp starts the host app again.
	OpenApp = openApp
	// HandOff opens Terminal running a command line, outside any app kemory
	// may run inside.
	HandOff = handOff
)

// ErrNoAppControl means kemory cannot quit or start the app on this OS.
var ErrNoAppControl = errors.New("kemory cannot quit or start this app on " + runtime.GOOS)

func command(timeout time.Duration, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return exec.CommandContext(ctx, name, args...).Output() //nolint:gosec // every caller passes a fixed command (ps, tasklist, osascript, taskkill, open) and fixed arguments
}

func isRunning(name string) bool {
	if !RewritesWhileRunning(name) {
		return false
	}
	switch runtime.GOOS {
	case "darwin":
		out, err := command(3*time.Second, "ps", "-axo", "comm=")
		if err != nil {
			return false
		}
		for _, line := range strings.Split(string(out), "\n") {
			if strings.HasSuffix(strings.TrimSpace(line), macOSApp) {
				return true
			}
		}
	case "windows":
		out, err := command(3*time.Second, "tasklist", "/FI", "IMAGENAME eq Claude.exe", "/NH")
		if err != nil {
			return false
		}
		return strings.Contains(strings.ToLower(string(out)), "claude.exe")
	}
	return false
}

func insideApp(name string) bool {
	if !RewritesWhileRunning(name) || runtime.GOOS != "darwin" {
		return false
	}
	pid := os.Getppid()
	for range 40 {
		if pid <= 1 {
			return false
		}
		out, err := command(3*time.Second, "ps", "-o", "ppid=,comm=", "-p", strconv.Itoa(pid))
		if err != nil {
			return false
		}
		ppid, comm, ok := strings.Cut(strings.TrimSpace(string(out)), " ")
		if !ok {
			return false
		}
		if strings.HasSuffix(strings.TrimSpace(comm), macOSApp) {
			return true
		}
		if pid, err = strconv.Atoi(ppid); err != nil {
			return false
		}
	}
	return false
}

func quitApp(name string, timeout time.Duration) error {
	if !RewritesWhileRunning(name) {
		return nil
	}
	var err error
	switch runtime.GOOS {
	case "darwin":
		_, err = command(10*time.Second, "osascript", "-e", `quit app "Claude"`)
	case "windows":
		// Without /F this asks the app to close, as clicking its close button does.
		_, err = command(10*time.Second, "taskkill", "/IM", "Claude.exe")
	default:
		return ErrNoAppControl
	}
	if err != nil {
		return err
	}
	for deadline := time.Now().Add(timeout); time.Now().Before(deadline); time.Sleep(500 * time.Millisecond) {
		if !RunningCheck(name) {
			return nil
		}
	}
	return errors.New("it is still running; it may be waiting for you to confirm quitting")
}

// CommandLine is the shell line that runs this kemory with args: plain
// "kemory" when that is the one on PATH, else its full path.
func CommandLine(args ...string) string {
	exe := "kemory"
	self, err := os.Executable()
	if err == nil {
		onPath, lookErr := exec.LookPath("kemory")
		if lookErr != nil || !samePath(onPath, self) {
			exe = self
		}
	}
	parts := []string{shellQuote(exe)}
	for _, a := range args {
		parts = append(parts, shellQuote(a))
	}
	return strings.Join(parts, " ")
}

func samePath(a, b string) bool {
	ra, errA := filepath.EvalSymlinks(a)
	rb, errB := filepath.EvalSymlinks(b)
	return errA == nil && errB == nil && ra == rb
}

func handOff(line string) error {
	if runtime.GOOS != "darwin" {
		return ErrNoAppControl
	}
	quoted := strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(line)
	_, err := command(10*time.Second, "osascript",
		"-e", `tell application "Terminal" to do script "`+quoted+`"`,
		"-e", `tell application "Terminal" to activate`)
	return err
}

func openApp(name string) error {
	if !RewritesWhileRunning(name) {
		return nil
	}
	if runtime.GOOS != "darwin" {
		return ErrNoAppControl
	}
	_, err := command(10*time.Second, "open", "-a", "Claude")
	return err
}
