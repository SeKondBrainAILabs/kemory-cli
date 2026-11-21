package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func setHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	return dir
}

// pythonWritten is a credentials file exactly as the Python CLI writes it
// (json.dumps(indent=2, sort_keys=True)).
const pythonWritten = `{
  "access_token": "at",
  "active_org_id": "",
  "client_id": "kemory-cli",
  "email": "a@b.c",
  "env": "prod",
  "expires_at": 1792000000.123456,
  "issuer": "https://accounts.sekondbrain.ai/realms/s9n",
  "kemory_url": "https://api.kemory.s9n.ai",
  "org_id": "org-1",
  "refresh_token": "rt",
  "version": 2
}`

func write(t *testing.T, body string) string {
	t.Helper()
	path, err := CredentialsPath("prod")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadsAFileThePythonCLIWrote(t *testing.T) {
	setHome(t)
	write(t, pythonWritten)
	c, err := LoadCredentials("prod")
	if err != nil || c == nil {
		t.Fatalf("load: %v %v", c, err)
	}
	if c.AccessToken != "at" || c.RefreshToken != "rt" || c.Email != "a@b.c" || c.OrgID != "org-1" || c.ExpiresAt != 1792000000.123456 {
		t.Fatalf("unexpected fields: %+v", c)
	}
}

func TestMissingFileIsNoCredentials(t *testing.T) {
	setHome(t)
	c, err := LoadCredentials("prod")
	if c != nil || err != nil {
		t.Fatalf("want nil, nil; got %v, %v", c, err)
	}
}

func TestTrailingBytesFromTheOldWriteRaceAreRecovered(t *testing.T) {
	setHome(t)
	write(t, pythonWritten+"\n  \"version\": 2\n}}")
	if c, _ := LoadCredentials("prod"); c == nil || c.AccessToken != "at" {
		t.Fatalf("leading object not recovered: %v", c)
	}
}

func TestUnusableFilesReadAsSignedOut(t *testing.T) {
	cases := map[string]string{
		"not json":         "garbage",
		"array":            "[1, 2]",
		"other version":    strings.Replace(pythonWritten, `"version": 2`, `"version": 1`, 1),
		"missing required": strings.Replace(pythonWritten, `"refresh_token": "rt",`, ``, 1),
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			setHome(t)
			write(t, body)
			if c, err := LoadCredentials("prod"); c != nil || err != nil {
				t.Fatalf("want nil, nil; got %+v, %v", c, err)
			}
		})
	}
}

// The list is spelled out rather than read from requiredFields so that
// dropping a field from requiredFields fails here. The Python CLI and the
// plugin hooks reject a file missing any of these.
func TestFileMissingAnyRequiredFieldReadsAsSignedOut(t *testing.T) {
	for _, field := range []string{"access_token", "refresh_token", "expires_at", "issuer", "client_id", "kemory_url"} {
		t.Run(field, func(t *testing.T) {
			var fields map[string]any
			if err := json.Unmarshal([]byte(pythonWritten), &fields); err != nil {
				t.Fatal(err)
			}
			delete(fields, field)
			body, err := json.Marshal(fields)
			if err != nil {
				t.Fatal(err)
			}
			setHome(t)
			write(t, string(body))
			if c, err := LoadCredentials("prod"); c != nil || err != nil {
				t.Fatalf("want nil, nil; got %+v, %v", c, err)
			}
		})
	}
}

func TestRetiredHostsAreRetargetedInMemoryOnly(t *testing.T) {
	setHome(t)
	body := strings.NewReplacer(
		"https://api.kemory.s9n.ai", "https://kemory.prod.apps.s9n.ai",
		"https://accounts.sekondbrain.ai/realms/s9n", "https://accounts.prod.apps.s9n.ai/realms/s9n",
	).Replace(pythonWritten)
	path := write(t, body)
	c, _ := LoadCredentials("prod")
	if c.KemoryURL != "https://api.kemory.s9n.ai" || c.Issuer != "https://accounts.sekondbrain.ai/realms/s9n" {
		t.Fatalf("not retargeted: %s %s", c.KemoryURL, c.Issuer)
	}
	onDisk, _ := os.ReadFile(path)
	if string(onDisk) != body {
		t.Fatal("a load must not write the file")
	}
}

func TestSelfHostedURLIsNeverRetargeted(t *testing.T) {
	setHome(t)
	write(t, strings.Replace(pythonWritten, "https://api.kemory.s9n.ai", "https://kemory.example.com", 1))
	if c, _ := LoadCredentials("prod"); c.KemoryURL != "https://kemory.example.com" {
		t.Fatalf("self-hosted URL changed to %s", c.KemoryURL)
	}
}

func TestSaveRoundTripsWithSortedKeysAndPrivateMode(t *testing.T) {
	home := setHome(t)
	c := &Credentials{
		AccessToken: "at", RefreshToken: "rt", ExpiresAt: float64(time.Now().Unix()) + 0.5,
		Issuer: "https://i", ClientID: "kemory-cli", KemoryURL: "https://k", Env: "prod",
	}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, ".kemory", "credentials-prod")
	raw, _ := os.ReadFile(path)
	var keys []string
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.Token()
	for dec.More() {
		k, _ := dec.Token()
		keys = append(keys, k.(string))
		var v any
		dec.Decode(&v)
	}
	for i := 1; i < len(keys); i++ {
		if keys[i-1] > keys[i] {
			t.Fatalf("keys not sorted: %v", keys)
		}
	}
	if runtime.GOOS != "windows" {
		if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
			t.Fatalf("mode %v, want 0600", info.Mode().Perm())
		}
	}
	back, _ := LoadCredentials("prod")
	if back == nil || *back != *c {
		t.Fatalf("round trip: %+v != %+v", back, c)
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Fatalf("temp files left behind: %v", entries)
	}
}

func TestExpiresWithin(t *testing.T) {
	c := &Credentials{ExpiresAt: float64(time.Now().Add(30 * time.Second).Unix())}
	if !c.ExpiresWithin(time.Minute) || c.ExpiresWithin(0) {
		t.Fatal("ExpiresWithin is wrong")
	}
}

func TestFileWithoutEnvDefaultsToDefaultEnvAndSavesBackToItsPath(t *testing.T) {
	setHome(t)
	path := write(t, strings.Replace(pythonWritten, "  \"env\": \"prod\",\n", "", 1))
	if strings.Contains(mustRead(t, path), `"env"`) {
		t.Fatal("fixture still has an env key")
	}
	c, err := LoadCredentialsFile(path)
	if err != nil || c == nil {
		t.Fatalf("load: %v %v", c, err)
	}
	if c.Env != DefaultEnv {
		t.Fatalf("Env = %q, want %q", c.Env, DefaultEnv)
	}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	saved, err := LoadCredentialsFile(path)
	if err != nil || saved == nil || saved.Env != DefaultEnv {
		t.Fatalf("reload from %s: %+v %v", path, saved, err)
	}
	dir := filepath.Dir(path)
	if _, err := os.Stat(filepath.Join(dir, "credentials-")); err == nil {
		t.Fatal("Save wrote to credentials- instead of the env's own file")
	}
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func sampleCredentials(env string) *Credentials {
	return &Credentials{
		AccessToken: "new-at", RefreshToken: "new-rt", ExpiresAt: 1792000000,
		Issuer: "https://i", ClientID: "kemory-cli", KemoryURL: "https://k", Env: env,
	}
}

func assertNoTempFiles(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Fatalf("temp file left behind: %s", e.Name())
		}
	}
}

func TestSaveFileFailsWhenTheParentDirectoryCannotBeCreated(t *testing.T) {
	home := setHome(t)
	blocker := filepath.Join(home, "blocker")
	if err := os.WriteFile(blocker, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := sampleCredentials("prod").SaveFile(filepath.Join(blocker, "sub", "credentials-prod")); err == nil {
		t.Fatal("SaveFile succeeded although its parent path is a regular file")
	}
	if got := mustRead(t, blocker); got != "not a directory" {
		t.Fatalf("blocking file changed: %q", got)
	}
	assertNoTempFiles(t, home)
}

func TestSaveFileKeepsTheExistingFileWhenTheDirectoryIsReadOnly(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("directory permissions do not stop writes on Windows or for root")
	}
	setHome(t)
	path := write(t, pythonWritten)
	dir := filepath.Dir(path)
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	if err := sampleCredentials("prod").SaveFile(path); err == nil {
		t.Fatal("SaveFile succeeded in a read-only directory")
	}
	if got := mustRead(t, path); got != pythonWritten {
		t.Fatalf("existing credentials changed:\n%s", got)
	}
	assertNoTempFiles(t, dir)
}

func TestSaveFileKeepsTheTargetAndLeavesNoTempFileWhenTheRenameFails(t *testing.T) {
	setHome(t)
	path, err := CredentialsPath("prod")
	if err != nil {
		t.Fatal(err)
	}
	// A non-empty directory at the target makes the final rename fail on
	// every OS, after the temp file was fully written.
	keep := filepath.Join(path, "keep")
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keep, []byte("untouched"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := sampleCredentials("prod").SaveFile(path); err == nil {
		t.Fatal("SaveFile succeeded although the target is a directory")
	}
	if got := mustRead(t, keep); got != "untouched" {
		t.Fatalf("target directory changed: %q", got)
	}
	assertNoTempFiles(t, filepath.Dir(path))
}
