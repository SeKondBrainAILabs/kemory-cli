package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// CredentialsVersion is the on-disk format version. A file with any other
// version is treated as absent.
const CredentialsVersion = 2

// Credentials are the cached OAuth tokens for one environment. The JSON shape
// is shared with the Python CLI and the Kemory Claude Code plugin's hooks, so
// field names and meanings must not change.
type Credentials struct {
	AccessToken  string  `json:"access_token"`
	RefreshToken string  `json:"refresh_token"`
	ExpiresAt    float64 `json:"expires_at"` // epoch seconds
	Issuer       string  `json:"issuer"`
	ClientID     string  `json:"client_id"`
	KemoryURL    string  `json:"kemory_url"`
	Env          string  `json:"env"`
	Email        string  `json:"email"`
	OrgID        string  `json:"org_id"`
	// ActiveOrgID is the user-selected org, sent as X-Organization-ID. Empty
	// means the server uses the token's org.
	ActiveOrgID string `json:"active_org_id"`
	Version     int    `json:"version"`
}

// ExpiresWithin reports whether the access token expires in the next d.
func (c *Credentials) ExpiresWithin(d time.Duration) bool {
	return float64(time.Now().Add(d).UnixNano())/1e9 >= c.ExpiresAt
}

// IsLocal reports whether this is a `login --local` API-key credential, which
// has no OAuth issuer to refresh against.
func (c *Credentials) IsLocal() bool {
	return c.Issuer == "local" || c.ClientID == "local"
}

// Dir returns ~/.kemory, creating it with mode 0700 if missing.
func Dir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".kemory")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	_ = os.Chmod(dir, 0o700) //nolint:gosec // a directory needs the execute bit; 0700 is owner-only
	return dir, nil
}

// CredentialsPath returns the credentials file for env.
func CredentialsPath(env string) (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "credentials-"+env), nil
}

// decodeCredentials parses a credential file. It reads only the leading JSON
// object: a concurrent-write race in older CLIs left files shaped
// `{...complete...}}`, and rejecting those reported a signed-in user
// as signed out.
func decodeCredentials(data []byte) (*Credentials, error) {
	var fields map[string]json.RawMessage
	dec := json.NewDecoder(bytes.NewReader(bytes.TrimLeft(data, " \t\r\n")))
	if err := dec.Decode(&fields); err != nil {
		return nil, err
	}
	// The Python CLI treats a file missing any of these as unusable.
	for _, key := range requiredFields {
		if _, ok := fields[key]; !ok {
			return nil, fmt.Errorf("credentials missing %q", key)
		}
	}
	raw, err := json.Marshal(fields)
	if err != nil {
		return nil, err
	}
	var c Credentials
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

var requiredFields = []string{"access_token", "refresh_token", "expires_at", "issuer", "client_id", "kemory_url"}

// LoadCredentials reads the credentials for env. It returns (nil, nil) when
// there is no usable file: missing, unreadable JSON, or another version.
func LoadCredentials(env string) (*Credentials, error) {
	path, err := CredentialsPath(env)
	if err != nil {
		return nil, err
	}
	return LoadCredentialsFile(path)
}

// LoadCredentialsFile is LoadCredentials for an explicit path.
func LoadCredentialsFile(path string) (*Credentials, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	c, err := decodeCredentials(data)
	if err != nil || c.Version != CredentialsVersion {
		// An unreadable or foreign-version file means "not signed in",
		// the same as a missing one; the user's fix is `kemory login`.
		return nil, nil //nolint:nilerr // deliberate, see above
	}
	if c.Env == "" {
		c.Env = DefaultEnv
	}
	// Retarget hosts this CLI has moved off. In memory only: a read must not
	// write. The new value reaches disk at the next save (token refresh).
	if to, ok := MigratedKemoryURLs[c.KemoryURL]; ok {
		c.KemoryURL = to
	}
	if to, ok := MigratedIssuers[c.Issuer]; ok {
		c.Issuer = to
	}
	return c, nil
}

// Save writes the credentials for c.Env atomically, mode 0600.
func (c *Credentials) Save() error {
	path, err := CredentialsPath(c.Env)
	if err != nil {
		return err
	}
	return c.SaveFile(path)
}

// SaveFile writes c to path through a uniquely named temp file and a rename.
// The CLI, the MCP bridge and the plugin's hooks all refresh this file; a
// shared temp name let two concurrent writers corrupt it.
func (c *Credentials) SaveFile(path string) error {
	c.Version = CredentialsVersion
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	// Marshal through a map so keys are sorted, matching the Python writer.
	raw, err := json.Marshal(c) //nolint:gosec // writing the tokens to the 0600 credentials file is this function's job
	if err != nil {
		return err
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	body, err := json.MarshalIndent(fields, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // no-op after a successful rename
	if err := tmp.Chmod(0o600); err != nil && !isUnsupported(err) {
		_ = tmp.Close() // already failing; the write error is the one to report
		return err
	}
	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close() // already failing; the write error is the one to report
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("save credentials: %w", err)
	}
	return nil
}

// isUnsupported reports a chmod failure that only means the platform has no
// Unix modes (Windows).
func isUnsupported(err error) bool {
	return errors.Is(err, errors.ErrUnsupported)
}
