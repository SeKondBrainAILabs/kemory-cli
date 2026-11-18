package hosts

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

const (
	macDesktop  = "/Applications/Claude.app/Contents/MacOS/Claude"
	macBundled  = "/Users/me/Library/Application Support/Claude/claude-code/2.0.0/claude.app/Contents/MacOS/claude"
	macHelper   = "/Applications/Claude.app/Contents/Frameworks/Claude Helper.app/Contents/MacOS/Claude Helper"
	psAllArgs   = "-axo comm="
	tasklistOut = "Claude.exe                    4242 Console                    1    150,000 K\r\n"
)

// fakeCommands replaces runCommand with a fake that answers from replies,
// keyed by the command line, and fails the test on any other command.
type fakeCommands struct {
	t       *testing.T
	replies map[string]fakeReply
	calls   []string
}

type fakeReply struct {
	out string
	err error
}

// swapSeams sets goos and parentPID, installs a fake runCommand and restores
// all of them, and RunningCheck, when the test ends.
func swapSeams(t *testing.T, osName string, replies map[string]fakeReply) *fakeCommands {
	t.Helper()
	f := &fakeCommands{t: t, replies: replies}
	oldGOOS, oldRun, oldPPID, oldCheck := goos, runCommand, parentPID, RunningCheck
	t.Cleanup(func() {
		goos, runCommand, parentPID, RunningCheck = oldGOOS, oldRun, oldPPID, oldCheck
	})
	goos = osName
	runCommand = func(_ time.Duration, name string, args ...string) ([]byte, error) {
		line := name + " " + strings.Join(args, " ")
		f.calls = append(f.calls, line)
		r, ok := f.replies[line]
		if !ok {
			f.t.Fatalf("unexpected command %q", line)
		}
		return []byte(r.out), r.err
	}
	return f
}

func (f *fakeCommands) assertNoCalls() {
	f.t.Helper()
	if len(f.calls) != 0 {
		f.t.Fatalf("commands run = %q, want none", f.calls)
	}
}

func TestIsRunningFindsClaudeAppInMacProcessList(t *testing.T) {
	for name, out := range map[string]string{
		"listed":       "/sbin/launchd\n" + macDesktop + "\n",
		"padded":       "/sbin/launchd\n  " + macDesktop + "  \n",
		"last, no \\n": macDesktop,
	} {
		t.Run(name, func(t *testing.T) {
			f := swapSeams(t, "darwin", map[string]fakeReply{"ps " + psAllArgs: {out: out}})
			if !isRunning("claude-desktop") {
				t.Fatalf("isRunning = false for %q, want true", out)
			}
			if want := []string{"ps -axo comm="}; !reflect.DeepEqual(f.calls, want) {
				t.Fatalf("commands = %q, want %q", f.calls, want)
			}
		})
	}
}

func TestIsRunningIgnoresBundledLowercaseClaudeAppAndHelpers(t *testing.T) {
	for name, out := range map[string]string{
		"bundled claude.app": "/sbin/launchd\n" + macBundled + "\n",
		"helper":             macHelper,
		"longer executable":  macDesktop + " Helper",
	} {
		t.Run(name, func(t *testing.T) {
			swapSeams(t, "darwin", map[string]fakeReply{"ps " + psAllArgs: {out: out}})
			if isRunning("claude-desktop") {
				t.Fatalf("isRunning = true for %q, want false", out)
			}
		})
	}
}

func TestIsRunningFalseWhenNothingMatchesOrPSFails(t *testing.T) {
	for name, reply := range map[string]fakeReply{
		"nothing matches": {out: "/sbin/launchd\n/usr/bin/login\n"},
		"empty output":    {},
		"ps fails":        {out: macDesktop + "\n", err: errors.New("ps failed")},
	} {
		t.Run(name, func(t *testing.T) {
			swapSeams(t, "darwin", map[string]fakeReply{"ps " + psAllArgs: reply})
			if isRunning("claude-desktop") {
				t.Fatalf("isRunning = true, want false")
			}
		})
	}
}

func TestIsRunningReadsTasklistOnWindows(t *testing.T) {
	const line = "tasklist /FI IMAGENAME eq Claude.exe /NH"
	for _, tc := range []struct {
		name  string
		reply fakeReply
		want  bool
	}{
		{"listed", fakeReply{out: tasklistOut}, true},
		{"listed in lowercase", fakeReply{out: strings.ToLower(tasklistOut)}, true},
		{"no tasks", fakeReply{out: "INFO: No tasks are running which match the specified criteria.\r\n"}, false},
		{"tasklist fails", fakeReply{out: tasklistOut, err: errors.New("tasklist failed")}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := swapSeams(t, "windows", map[string]fakeReply{line: tc.reply})
			if got := isRunning("claude-desktop"); got != tc.want {
				t.Fatalf("isRunning = %v, want %v", got, tc.want)
			}
			if want := []string{line}; !reflect.DeepEqual(f.calls, want) {
				t.Fatalf("commands = %q, want %q", f.calls, want)
			}
		})
	}
}

func TestIsRunningRunsNothingForOtherHostsOrLinux(t *testing.T) {
	f := swapSeams(t, "darwin", nil)
	if isRunning("cursor") {
		t.Fatal("isRunning(cursor) on darwin = true, want false")
	}
	goos = "linux"
	if isRunning("claude-desktop") {
		t.Fatal("isRunning(claude-desktop) on linux = true, want false")
	}
	f.assertNoCalls()
}

// psParent is the command line insideApp uses to look up a process's parent.
func psParent(pid int) string { return "ps -o ppid=,comm= -p " + strconv.Itoa(pid) }

func TestInsideAppFindsClaudeAppAmongAncestors(t *testing.T) {
	f := swapSeams(t, "darwin", map[string]fakeReply{
		psParent(500): {out: "  400 /bin/zsh\n"},
		psParent(400): {out: "    1 " + macDesktop + "\n"},
	})
	parentPID = func() int { return 500 }
	if !insideApp("claude-desktop") {
		t.Fatal("insideApp = false, want true")
	}
	if want := []string{psParent(500), psParent(400)}; !reflect.DeepEqual(f.calls, want) {
		t.Fatalf("commands = %q, want %q", f.calls, want)
	}
}

func TestInsideAppStopsAtLaunchdOrBadPSOutput(t *testing.T) {
	for _, tc := range []struct {
		name    string
		replies map[string]fakeReply
		ppid    int
		want    []string
	}{
		{
			name: "walk ends at launchd without asking about pid 1",
			ppid: 500,
			replies: map[string]fakeReply{
				psParent(500): {out: "400 /bin/zsh"},
				psParent(400): {out: "1 /usr/bin/login"},
			},
			want: []string{psParent(500), psParent(400)},
		},
		{
			name: "bundled lowercase claude.app is not the app",
			ppid: 500,
			replies: map[string]fakeReply{
				psParent(500): {out: "400 /bin/zsh"},
				psParent(400): {out: "1 " + macBundled},
			},
			want: []string{psParent(500), psParent(400)},
		},
		{name: "parent is launchd", ppid: 1},
		{
			name:    "ps fails",
			ppid:    500,
			replies: map[string]fakeReply{psParent(500): {err: errors.New("ps failed")}},
			want:    []string{psParent(500)},
		},
		{
			name:    "output has no space",
			ppid:    500,
			replies: map[string]fakeReply{psParent(500): {out: "garbage"}},
			want:    []string{psParent(500)},
		},
		{
			name:    "ppid is not a number",
			ppid:    500,
			replies: map[string]fakeReply{psParent(500): {out: "abc /bin/zsh"}},
			want:    []string{psParent(500)},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := swapSeams(t, "darwin", tc.replies)
			parentPID = func() int { return tc.ppid }
			if insideApp("claude-desktop") {
				t.Fatal("insideApp = true, want false")
			}
			if len(tc.want) == 0 {
				f.assertNoCalls()
			} else if !reflect.DeepEqual(f.calls, tc.want) {
				t.Fatalf("commands = %q, want %q", f.calls, tc.want)
			}
		})
	}
}

func TestInsideAppGivesUpAfterFortyParents(t *testing.T) {
	f := swapSeams(t, "darwin", map[string]fakeReply{psParent(999): {out: "999 /bin/zsh"}})
	parentPID = func() int { return 999 }
	if insideApp("claude-desktop") {
		t.Fatal("insideApp = true, want false")
	}
	if len(f.calls) != 40 {
		t.Fatalf("ps calls = %d, want 40", len(f.calls))
	}
}

func TestInsideAppOnlyChecksClaudeDesktopOnMac(t *testing.T) {
	f := swapSeams(t, "linux", nil)
	parentPID = func() int { return 500 }
	for _, goosName := range []string{"linux", "windows"} {
		goos = goosName
		if insideApp("claude-desktop") {
			t.Fatalf("insideApp(claude-desktop) on %s = true, want false", goosName)
		}
	}
	goos = "darwin"
	if insideApp("cursor") {
		t.Fatal("insideApp(cursor) on darwin = true, want false")
	}
	f.assertNoCalls()
}

func TestQuitAppAsksTheAppToQuitPerOS(t *testing.T) {
	for _, tc := range []struct {
		goos string
		line string
	}{
		{"darwin", `osascript -e quit app "Claude"`},
		{"windows", "taskkill /IM Claude.exe"},
	} {
		t.Run(tc.goos, func(t *testing.T) {
			f := swapSeams(t, tc.goos, map[string]fakeReply{tc.line: {}})
			RunningCheck = func(string) bool { return false }
			if err := quitApp("claude-desktop", time.Second); err != nil {
				t.Fatalf("quitApp = %v, want nil", err)
			}
			if want := []string{tc.line}; !reflect.DeepEqual(f.calls, want) {
				t.Fatalf("commands = %q, want %q", f.calls, want)
			}
		})
	}

	t.Run("linux has no app control", func(t *testing.T) {
		f := swapSeams(t, "linux", nil)
		if err := quitApp("claude-desktop", time.Second); !errors.Is(err, ErrNoAppControl) {
			t.Fatalf("quitApp = %v, want ErrNoAppControl", err)
		}
		f.assertNoCalls()
	})

	t.Run("other hosts are left alone", func(t *testing.T) {
		f := swapSeams(t, "darwin", nil)
		if err := quitApp("cursor", time.Second); err != nil {
			t.Fatalf("quitApp(cursor) = %v, want nil", err)
		}
		f.assertNoCalls()
	})
}

func TestQuitAppReportsCommandErrorsAndStillRunning(t *testing.T) {
	t.Run("quit command fails", func(t *testing.T) {
		quitErr := errors.New("osascript failed")
		swapSeams(t, "darwin", map[string]fakeReply{`osascript -e quit app "Claude"`: {err: quitErr}})
		RunningCheck = func(string) bool { t.Fatal("RunningCheck called after the quit command failed"); return false }
		if err := quitApp("claude-desktop", time.Second); !errors.Is(err, quitErr) {
			t.Fatalf("quitApp = %v, want %v", err, quitErr)
		}
	})

	t.Run("app still running at the deadline", func(t *testing.T) {
		swapSeams(t, "darwin", map[string]fakeReply{`osascript -e quit app "Claude"`: {}})
		RunningCheck = func(string) bool { return true }
		err := quitApp("claude-desktop", 0)
		const want = "it is still running; it may be waiting for you to confirm quitting"
		if err == nil || err.Error() != want {
			t.Fatalf("quitApp = %v, want %q", err, want)
		}
	})
}

func TestSamePathResolvesSymlinks(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a")
	b := filepath.Join(dir, "b")
	for _, p := range []string{a, b} {
		if err := os.WriteFile(p, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	missing := filepath.Join(dir, "missing")

	if !samePath(a, a) {
		t.Error("samePath(a, a) = false, want true")
	}
	if samePath(a, b) {
		t.Error("samePath(a, b) = true, want false")
	}
	if samePath(missing, missing) {
		t.Error("samePath(missing, missing) = true, want false")
	}
	// Creating a symlink needs privileges on Windows.
	if runtime.GOOS != "windows" {
		link := filepath.Join(dir, "link")
		if err := os.Symlink(a, link); err != nil {
			t.Fatal(err)
		}
		if !samePath(link, a) {
			t.Error("samePath(link, a) = false, want true")
		}
	}
}
