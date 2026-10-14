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
