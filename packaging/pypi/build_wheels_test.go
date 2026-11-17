package pypi_test

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

const wheelVersion = "1.2.3"

type wheelTarget struct {
	name  string
	wheel string
	tags  []string
	// script is the binary's name inside the wheel and the archive.
	script string
}

// The expected wheels are written out here on purpose: the test must not read
// them from build_wheels.py.
var wheelTargets = []wheelTarget{
	{"macos-arm64", "kemory-1.2.3-py3-none-macosx_12_0_arm64.whl", []string{"macosx_12_0_arm64"}, "kemory"},
	{"macos-x64", "kemory-1.2.3-py3-none-macosx_12_0_x86_64.whl", []string{"macosx_12_0_x86_64"}, "kemory"},
	{
		"linux-x64",
		"kemory-1.2.3-py3-none-manylinux_2_17_x86_64.manylinux2014_x86_64.musllinux_1_1_x86_64.whl",
		[]string{"manylinux_2_17_x86_64", "manylinux2014_x86_64", "musllinux_1_1_x86_64"},
		"kemory",
	},
	{
		"linux-arm64",
		"kemory-1.2.3-py3-none-manylinux_2_17_aarch64.manylinux2014_aarch64.musllinux_1_1_aarch64.whl",
		[]string{"manylinux_2_17_aarch64", "manylinux2014_aarch64", "musllinux_1_1_aarch64"},
		"kemory",
	},
	{"windows-x64", "kemory-1.2.3-py3-none-win_amd64.whl", []string{"win_amd64"}, "kemory.exe"},
}

func python(t *testing.T) string {
	t.Helper()
	for _, name := range []string{"python3", "python"} {
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
	}
	if os.Getenv("GITHUB_ACTIONS") == "true" {
		t.Fatal("python3 is not installed")
	}
	t.Skip("python3 is not installed")
	return ""
}

func writeTarGz(t *testing.T, path, name string, data []byte) {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(data)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeZip(t *testing.T, path, name string, data []byte) {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
}

// writeArchives writes the five GoReleaser archives build_wheels.py reads.
// binaries maps a target name to the bytes of its binary.
func writeArchives(t *testing.T, dist string, binaries map[string][]byte) {
	t.Helper()
	for _, tg := range wheelTargets {
		if tg.name == "windows-x64" {
			writeZip(t, filepath.Join(dist, "kemory-"+tg.name+".zip"), tg.script, binaries[tg.name])
			continue
		}
		writeTarGz(t, filepath.Join(dist, "kemory-"+tg.name+".tar.gz"), tg.script, binaries[tg.name])
	}
}

func buildWheels(t *testing.T, py, dist, out string) {
	t.Helper()
	cmd := exec.Command(py, "build_wheels.py", "--version", wheelVersion, "--dist", dist, "--out", out)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build_wheels.py: %v\n%s", err, output)
	}
}

func readEntry(t *testing.T, f *zip.File) []byte {
	t.Helper()
	rc, err := f.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestBuildWheelsTagsBinaryAndRecordForEveryTarget(t *testing.T) {
	py := python(t)
	t.Setenv("HOME", t.TempDir())
	dist := t.TempDir()
	out := filepath.Join(dist, "wheels")

	binaries := map[string][]byte{}
	for _, tg := range wheelTargets {
		binaries[tg.name] = []byte("fake-kemory-" + tg.name)
	}
	writeArchives(t, dist, binaries)
	buildWheels(t, py, dist, out)

	built, err := filepath.Glob(filepath.Join(out, "*.whl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(built) != len(wheelTargets) {
		t.Fatalf("built %d wheels, want %d: %v", len(built), len(wheelTargets), built)
	}

	for _, tg := range wheelTargets {
		t.Run(tg.name, func(t *testing.T) {
			zr, err := zip.OpenReader(filepath.Join(out, tg.wheel))
			if err != nil {
				t.Fatalf("wheel %s: %v", tg.wheel, err)
			}
			defer zr.Close()

			entries := map[string]*zip.File{}
			for _, f := range zr.File {
				entries[f.Name] = f
			}
			distInfo := "kemory-" + wheelVersion + ".dist-info/"

			wheelMeta := string(readEntry(t, mustEntry(t, entries, distInfo+"WHEEL")))
			var gotTags []string
			for _, line := range strings.Split(wheelMeta, "\n") {
				if tag, ok := strings.CutPrefix(line, "Tag: "); ok {
					gotTags = append(gotTags, tag)
				}
			}
			var wantTags []string
			for _, tag := range tg.tags {
				wantTags = append(wantTags, "py3-none-"+tag)
			}
			if strings.Join(gotTags, ",") != strings.Join(wantTags, ",") {
				t.Errorf("WHEEL tags = %v, want %v", gotTags, wantTags)
			}
			if !strings.Contains(wheelMeta, "Root-Is-Purelib: false\n") {
				t.Errorf("WHEEL lacks Root-Is-Purelib: false:\n%s", wheelMeta)
			}

			meta := string(readEntry(t, mustEntry(t, entries, distInfo+"METADATA")))
			for _, want := range []string{"Name: kemory\n", "Version: " + wheelVersion + "\n"} {
				if !strings.Contains(meta, want) {
					t.Errorf("METADATA lacks %q", want)
				}
			}

			var scripts []string
			for name := range entries {
				if strings.HasPrefix(name, "kemory-"+wheelVersion+".data/scripts/") {
					scripts = append(scripts, name)
				}
			}
			wantScript := "kemory-" + wheelVersion + ".data/scripts/" + tg.script
			if len(scripts) != 1 || scripts[0] != wantScript {
				t.Fatalf("scripts = %v, want only %s", scripts, wantScript)
			}
			script := entries[wantScript]
			if got := readEntry(t, script); !bytes.Equal(got, binaries[tg.name]) {
				t.Errorf("script bytes = %q, want %q", got, binaries[tg.name])
			}
			attrs := script.ExternalAttrs >> 16
			if attrs&0o777 != 0o755 {
				t.Errorf("script mode = %o, want 755", attrs&0o777)
			}
			if attrs&0o100000 == 0 {
				t.Errorf("script is not a regular file: attrs %o", attrs)
			}

			checkRecord(t, zr, entries, distInfo+"RECORD")
		})
	}
}

func mustEntry(t *testing.T, entries map[string]*zip.File, name string) *zip.File {
	t.Helper()
	f, ok := entries[name]
	if !ok {
		t.Fatalf("wheel has no %s", name)
	}
	return f
}

func checkRecord(t *testing.T, zr *zip.ReadCloser, entries map[string]*zip.File, recordPath string) {
	t.Helper()
	text := string(readEntry(t, mustEntry(t, entries, recordPath)))
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	if len(lines) != len(zr.File) {
		t.Errorf("RECORD has %d lines, wheel has %d entries:\n%s", len(lines), len(zr.File), text)
	}
	seen := map[string]bool{}
	for _, line := range lines {
		if line == recordPath+",," {
			seen[recordPath] = true
			continue
		}
		parts := strings.Split(line, ",")
		if len(parts) != 3 {
			t.Errorf("RECORD line %q is not path,hash,size", line)
			continue
		}
		f, ok := entries[parts[0]]
		if !ok {
			t.Errorf("RECORD names %s, which is not in the wheel", parts[0])
			continue
		}
		if seen[parts[0]] {
			t.Errorf("RECORD lists %s twice", parts[0])
		}
		seen[parts[0]] = true
		data := readEntry(t, f)
		sum := sha256.Sum256(data)
		want := fmt.Sprintf("%s,sha256=%s,%d", parts[0], base64.RawURLEncoding.EncodeToString(sum[:]), len(data))
		if line != want {
			t.Errorf("RECORD line = %q, want %q", line, want)
		}
	}
	var missing []string
	for name := range entries {
		if !seen[name] {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("RECORD does not list %v", missing)
	}
}

func hostTarget() (string, bool) {
	switch runtime.GOOS + "/" + runtime.GOARCH {
	case "darwin/arm64":
		return "macos-arm64", true
	case "darwin/amd64":
		return "macos-x64", true
	case "linux/amd64":
		return "linux-x64", true
	case "linux/arm64":
		return "linux-arm64", true
	case "windows/amd64":
		return "windows-x64", true
	}
	return "", false
}

func TestHostWheelInstallsAndRunsKemoryVersion(t *testing.T) {
	if testing.Short() {
		t.Skip("installs a wheel: skipped under -short")
	}
	host, ok := hostTarget()
	if !ok {
		t.Skipf("no wheel for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	py := python(t)
	// HOME stays as it is: pointing it at a temp dir would put the Go module
	// cache there, and its read-only files break the temp dir's cleanup. Nothing
	// here reads ~/.kemory.
	dist := t.TempDir()
	out := filepath.Join(dist, "wheels")

	binName, venvBin := "kemory", "bin"
	if runtime.GOOS == "windows" {
		binName, venvBin = "kemory.exe", "Scripts"
	}
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Fatalf("no go binary: %v", err)
	}
	built := filepath.Join(t.TempDir(), binName)
	build := exec.Command(goBin, "build", "-o", built,
		"-ldflags", "-X github.com/SeKondBrainAILabs/kemory-cli/internal/version.Version="+wheelVersion,
		"github.com/SeKondBrainAILabs/kemory-cli/cmd/kemory")
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, output)
	}
	realBin, err := os.ReadFile(built)
	if err != nil {
		t.Fatal(err)
	}

	binaries := map[string][]byte{}
	for _, tg := range wheelTargets {
		binaries[tg.name] = []byte("fake-kemory-" + tg.name)
	}
	binaries[host] = realBin
	writeArchives(t, dist, binaries)
	buildWheels(t, py, dist, out)

	var wheel string
	for _, tg := range wheelTargets {
		if tg.name == host {
			wheel = filepath.Join(out, tg.wheel)
		}
	}

	venv := filepath.Join(t.TempDir(), "venv")
	run := func(name string, args ...string) string {
		t.Helper()
		output, err := exec.Command(name, args...).CombinedOutput()
		if err != nil {
			t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, output)
		}
		return string(output)
	}
	run(py, "-m", "venv", venv)
	venvPython := filepath.Join(venv, venvBin, "python")
	if runtime.GOOS == "windows" {
		venvPython += ".exe"
	}
	run(venvPython, "-m", "pip", "install", "--no-index", "--no-deps", wheel)

	if got := run(filepath.Join(venv, venvBin, binName), "--version"); !strings.Contains(got, wheelVersion) {
		t.Errorf("kemory --version = %q, want it to contain %s", got, wheelVersion)
	}
}
