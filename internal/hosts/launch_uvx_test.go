package hosts

import (
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestLaunchHelperProcess prints LaunchFor as JSON. It only does anything when
// re-run by launchFromCopy, because os.Executable is the only input to
// runningUnderUVX that a test cannot set in-process.
func TestLaunchHelperProcess(t *testing.T) {
	if os.Getenv("KEMORY_LAUNCH_HELPER") != "1" {
		t.Skip("helper for the uvx launch tests")
	}
	if err := json.NewEncoder(os.Stdout).Encode(LaunchFor("prod")); err != nil {
		t.Fatal(err)
	}
}

// launchFromCopy copies the test binary to dir/rel and returns the Launch it
// computes when run from there with UV set to uv.
func launchFromCopy(t *testing.T, rel, uv string) Launch {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatal(err)
	}
	src, err := os.Open(exe)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(out, src); err != nil {
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(dst, "-test.run=^TestLaunchHelperProcess$")
	cmd.Env = append(os.Environ(), "KEMORY_LAUNCH_HELPER=1", "KEMORY_LAUNCHER=", "UV="+uv)
	data, err := cmd.Output()
	if err != nil {
		t.Fatalf("helper: %v", err)
	}
	var l Launch
	if err := json.Unmarshal([]byte(strings.SplitN(string(data), "\n", 2)[0]), &l); err != nil {
		t.Fatalf("helper output %q: %v", data, err)
	}
	return l
}

func TestLaunchUnderUVXArchiveUsesUvx(t *testing.T) {
	l := launchFromCopy(t, "uv/archive-v0/abc/bin/kemory.test", "/usr/bin/uv")
	if l.Command != "uvx" || strings.Join(l.Args, " ") != "kemory --env prod mcp serve" {
		t.Fatalf("launch: %+v", l)
	}
}

func TestLaunchWithUVOutsideArchiveUsesKemory(t *testing.T) {
	l := launchFromCopy(t, "bin/kemory.test", "/usr/bin/uv")
	if l.Command != "kemory" || strings.Join(l.Args, " ") != "--env prod mcp serve" {
		t.Fatalf("launch: %+v", l)
	}
}

func TestLaunchInArchiveWithoutUVUsesKemory(t *testing.T) {
	l := launchFromCopy(t, "uv/archive-v0/abc/bin/kemory.test", "")
	if l.Command != "kemory" {
		t.Fatalf("launch: %+v", l)
	}
}
