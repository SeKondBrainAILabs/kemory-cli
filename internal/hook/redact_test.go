package hook

import (
	"strings"
	"testing"
)

// The tables below carry the redaction cases of the Kemory plugin's test suite
// (test/test_hooks.py in kemory-plugin at commit d7f8e2da), so a change to one
// copy of the patterns that the other does not follow fails here. Credential
// shaped values are built by concatenation, so the source holds no complete
// key literal.

// wantRedacted fails unless in comes back with a placeholder and none of the
// fragments that must be gone.
func wantRedacted(t *testing.T, in string, gone ...string) {
	t.Helper()
	got := Redact(in)
	if !strings.Contains(got, placeholder) {
		t.Errorf("Redact(%q) = %q, want a %s", in, got, placeholder)
	}
	for _, g := range gone {
		if strings.Contains(got, g) {
			t.Errorf("Redact(%q) = %q, still holds %q", in, got, g)
		}
	}
}

func TestRedactRemovesSecretShapes(t *testing.T) {
	awsSecret := "wJalrXUtnFEMI/K7MDENG/" + "bPxRfiCYEXAMPLEKEY"
	skKeys := []string{
		"sk-ant-" + "api03-" + strings.Repeat("Ab1", 30) + "-xyzAA",
		"sk-proj-" + strings.Repeat("Q7x", 20),
		"sk-" + "svcacct-" + strings.Repeat("Zz9_", 10),
		"sk-" + strings.Repeat("A1b2", 5) + "-tail_" + strings.Repeat("B", 8),
	}
	vendorKeys := []string{
		"github_pat_" + "A1b2C3d4E5f6G7h8I9j0K1" + "_" + strings.Repeat("x9Y", 20),
		"sk_live_" + "51H" + strings.Repeat("q", 30),
		"rk_live_" + strings.Repeat("R7", 15),
		"sk_test_" + strings.Repeat("4eC", 10),
		"rk_test_" + strings.Repeat("Tt9", 10),
		"AIza" + "SyA" + strings.Repeat("b_-9", 8),
		"glpat-" + "abcdefghij1234567890",
	}

	type secretCase struct {
		name, in string
		gone     []string
	}
	var cases []secretCase

	for _, in := range []string{
		"export GH_TOKEN=ghp_AAAAAAAAAAAAAAAAAAAAAAAA",
		`api_key="sk-abcdefghij1234567890"`,
		"password: hunter2supersecret",
		"AKIAIOSFODNN7EXAMPLE",
		"xoxb-1234567890-abcdefghij",
		// Plain keys, one after a space and one glued to a word.
		"my key is " + "sk-" + "abcdefghij1234567890",
		"url ?q=key%3D" + "sk-" + "abcdefghij1234567890",
	} {
		cases = append(cases, secretCase{name: "secret " + in, in: in})
	}

	for _, key := range skKeys {
		cases = append(cases, secretCase{"dashed sk key " + key[:8], "my key is " + key + " and the call 401s", []string{key[len(key)-12:]}})
	}

	for _, key := range vendorKeys {
		cases = append(cases, secretCase{"vendor key " + key[:8], "my key is " + key + " and the call 401s", []string{key[len(key)-12:], key}})
		// A JSON-escaped paste puts a literal backslash and a letter, or four
		// hex digits, right before the key; the last one is lowercase.
		for _, escape := range []string{`\n`, `\t`, `\u003c`} {
			cases = append(cases, secretCase{"vendor key " + key[:8] + " after " + escape, `{"env": "A=1` + escape + key + `"}`, []string{key[len(key)-12:]}})
		}
	}
	for _, escape := range []string{"%3D", "%3d"} {
		cases = append(cases, secretCase{"stripe key after " + escape, "url ?q=key" + escape + vendorKeys[1], []string{vendorKeys[1][len(vendorKeys[1])-12:]}})
	}

	// A quoted key has its closing quote between the keyword and the
	// separator. The fourth line is JSON-escaped text.
	for _, c := range []struct{ in, value string }{
		{`{"api_key": "kmy_live_9f8e7d6c5b4a3210"}`, "kmy_live_9f8e7d6c5b4a3210"},
		{`{"password": "Tr0ub4dor-and-3"}`, "Tr0ub4dor-and-3"},
		{`'access_token': 'q8Zr2LmN0pX4vB7cW1'`, "q8Zr2LmN0pX4vB7cW1"},
		{`{\"api_key\": \"kmy_live_9f8e7d6c5b4a3210\"}`, "kmy_live_9f8e7d6c5b4a3210"},
		{`"db_password" : "Tr0ub4dor-and-3"`, "Tr0ub4dor-and-3"},
	} {
		cases = append(cases, secretCase{"quoted key " + c.in, c.in, []string{c.value}})
	}

	// Environment-style names that end in KEY, and passwords in URLs.
	for _, c := range []struct{ in, value string }{
		{"export AWS_SECRET_ACCESS_KEY=" + awsSecret, awsSecret},
		{"DATABASE_URL=postgres://app:" + "S3cretPassw0rd@db.internal:5432/app", "S3cretPassw0rd"},
		{"SECRET_KEY=" + "django-insecure-" + "abcdef123", "django-insecure-abcdef123"},
		{"JWT_SECRET_SIGNING_KEY=" + "abcdef" + "123456", "abcdef123456"},
		{"PRIVATE_KEY=" + "0xabcdef" + "0123456789", "0xabcdef0123456789"},
		{"AccountName=x;AccountKey=" + "abc123" + "def456==;", "abc123def456"},
		{"redis://:" + "hunter2pass@cache:6379/0", "hunter2pass"},
		{"postgres://app:p@ss@db/app", "p@ss"},
		{"postgres://user@myserver:" + "S3cretPassw0rd" + "@myserver.postgres.database.azure.com:5432/db", "S3cretPassw0rd"},
		{"https://me@example.com:" + "ghtoken123456@git.example.com/repo.git", "ghtoken123456"},
		{`{"aws_secret_access_key": "` + awsSecret + `"}`, awsSecret},
	} {
		cases = append(cases, secretCase{"env or url " + c.in, c.in, []string{c.value}})
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { wantRedacted(t, c.in, c.gone...) })
	}
}

func TestRedactKeepsProse(t *testing.T) {
	for _, in := range []string{
		"the token expired yesterday, please regenerate",
		"our secret sauce is caching",
		"add a password field to the signup form",
		"why does the api_key check fail on staging",
		"multi-task-learning-pipeline-tuning",
		"check the disk-space-monitoring-dashboard",
		`the "password" field is required`,
		`set "token": null in the config`,
		"we need a token for this",
		// Shapes the URL password and SECRET...KEY patterns must leave alone.
		"ssh://git@github.com:22/org/repo",
		"git@github.com:org/repo.git",
		"see http://example.com:8080/path@v2",
		"https://user@host/path",
		"mailto:someone@example.com",
		"http://localhost:3000?email=a@b.com",
		"the secret key is stored in vault",
		"secret_keyboard=abcdefgh",
		"the private key is in ~/.ssh",
		// Vendor key prefixes in ordinary identifiers and prose.
		"risk_test_validateEverythingHere",
		"export GITHUB_PAT_ROTATION_REMINDER_DAYS=30",
		"github_pat_expiry_warning_threshold",
		"the Stripe sk_live_ key goes in the vault",
		"task_test_" + "abcdefghijklmnopqrstuvwxyz",
		"RK_LIVE_" + "RESTRICTEDKEYFORWEBHOOKS",
	} {
		t.Run(in, func(t *testing.T) {
			if got := Redact(in); got != in {
				t.Errorf("Redact(%q) = %q, want it unchanged", in, got)
			}
		})
	}
}

func TestRedactKeepsURLUserAndHost(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"DATABASE_URL=postgres://app:" + "S3cretPassw0rd@db.internal:5432/app",
			"DATABASE_URL=postgres://app:[REDACTED]@db.internal:5432/app"},
		{"redis://:" + "hunter2pass@cache:6379/0", "redis://:[REDACTED]@cache:6379/0"},
		{"postgres://app:p@ss@db/app", "postgres://app:[REDACTED]@db/app"},
		// The user name has an @ in it.
		{"postgres://user@myserver:" + "S3cretPassw0rd" + "@myserver.postgres.database.azure.com:5432/db",
			"postgres://user@myserver:[REDACTED]@myserver.postgres.database.azure.com:5432/db"},
		{"https://me@example.com:" + "ghtoken123456@git.example.com/repo.git",
			"https://me@example.com:[REDACTED]@git.example.com/repo.git"},
	} {
		t.Run(c.in, func(t *testing.T) {
			if got := Redact(c.in); got != c.want {
				t.Errorf("Redact(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

// A dashed sk- name followed by a secret another pattern catches: the name
// must not hide that secret.
func TestRedactDashedSkNameDoesNotHideTheSecretAfterIt(t *testing.T) {
	for _, c := range []struct{ in, gone string }{
		{"sk-prod-db-" + "password: " + "hunter2hunter2", "hunter2hunter2"},
		{"export SK-STAGING-" + "API_TOKEN=" + "abcdef123456", "abcdef123456"},
		{"sk-" + strings.Repeat("a", 16) + "-token=" + "supersecretvalue", "supersecretvalue"},
		{"sk-" + "build-cache-key-" + "eyJ" + strings.Repeat("h", 12) + "." + strings.Repeat("p", 12) + "." + strings.Repeat("SIG", 4), strings.Repeat("SIG", 4)},
		{"sk-" + "deploy-host-ssh-key" + "-----BEGIN RSA " + "PRIVATE KEY-----", "PRIVATE KEY-----"},
		{"sk-proj-" + strings.Repeat("Q7x", 5) + "sk-" + strings.Repeat("A", 16) + "_" + strings.Repeat("TAIL", 3), strings.Repeat("TAIL", 3)},
	} {
		t.Run(c.in[:16], func(t *testing.T) { wantRedacted(t, c.in, c.gone) })
	}
}

// A vendor key pattern must not swallow, or be swallowed by, a following
// password= assignment.
func TestRedactVendorKeyDoesNotHideTheSecretAfterIt(t *testing.T) {
	wantRedacted(t, "glpat-"+strings.Repeat("x", 20)+"-password="+"hunter2hunter2",
		"hunter2hunter2", strings.Repeat("x", 20))
}

// Fake key material only: the repo is public.
var keyBody = []string{"MIIEpAIBAAKCAQEA" + strings.Repeat("q", 60), strings.Repeat("zzzz", 16) + "wxyz"}

func pem(label, headers string, body []string) string {
	return "-----BEGIN " + label + "-----\n" + headers + strings.Join(body, "\n") + "\n-----END " + label + "-----"
}

func TestRedactRemovesWholePrivateKeyBlocks(t *testing.T) {
	pgpBody := append(append([]string{}, keyBody...), "=abcd")
	blocks := []struct{ name, block string }{
		{"rsa", pem("RSA PRIVATE KEY", "", keyBody)},
		{"pkcs8", pem("PRIVATE KEY", "", keyBody)},
		{"ec", pem("EC PRIVATE KEY", "", keyBody)},
		{"openssh", pem("OPENSSH PRIVATE KEY", "", keyBody)},
		{"encrypted pkcs8", pem("ENCRYPTED PRIVATE KEY", "", keyBody)},
		{"encrypted rsa", pem("RSA PRIVATE KEY",
			"Proc-Type: 4,ENCRYPTED\nDEK-Info: AES-128-CBC,"+strings.Repeat("0123456789ABCDEF", 2)+"\n\n", keyBody)},
		{"pgp", pem("PGP PRIVATE KEY BLOCK", "Version: GnuPG v2\n\n", pgpBody)},
		{"space-joined", "-----BEGIN RSA PRIVATE KEY----- " + strings.Join(keyBody, " ") + " -----END RSA PRIVATE KEY-----"},
	}
	for _, c := range blocks {
		t.Run(c.name, func(t *testing.T) {
			got := Redact("why does this key fail: " + c.block + " thanks")
			if want := "why does this key fail: [REDACTED] thanks"; got != want {
				t.Errorf("Redact = %q, want %q", got, want)
			}
		})
	}

	// The keyword alternative of secret starts earlier than the BEGIN line and
	// would otherwise eat only "-----BEGIN". Where the keyword itself is not
	// matched (JWT_SECRET=, client_secret:) the redaction of the block leaves
	// "[REDACTED]" as the value, which secret then takes with the keyword.
	for _, c := range []struct{ prefix, want string }{
		{"JWT_SECRET=", "set JWT_[REDACTED] done"},
		{"password=", "set [REDACTED] done"},
		{"password: ", "set [REDACTED] done"},
		{"token: ", "set [REDACTED] done"},
		{`client_secret: "`, "set client_[REDACTED] done"},
	} {
		t.Run("after "+c.prefix, func(t *testing.T) {
			got := Redact("set " + c.prefix + pem("RSA PRIVATE KEY", "", keyBody) + " done")
			if got != c.want {
				t.Errorf("Redact = %q, want %q", got, c.want)
			}
		})
	}

	// A service-account file pasted whole: newlines are a literal backslash-n.
	// The key name goes too: after the block pass, "private_key": "[REDACTED]
	// is itself a keyword and a value.
	head := `{"private_key": "-----BEGIN PRIVATE KEY-----\n` + keyBody[0] + `\n` + keyBody[1]
	t.Run("json escaped, terminated", func(t *testing.T) {
		got := Redact(head + `\n-----END PRIVATE KEY-----\n", "client_email": "a@b.c"}`)
		if want := `{"[REDACTED]", "client_email": "a@b.c"}`; got != want {
			t.Errorf("Redact = %q, want %q", got, want)
		}
	})
	t.Run("json escaped, unterminated", func(t *testing.T) {
		if got, want := Redact(head), `{"[REDACTED]`; got != want {
			t.Errorf("Redact = %q, want %q", got, want)
		}
	})

	// A truncated paste has no END line: the base64 after BEGIN still goes,
	// ordinary text after it stays.
	b1, b2 := strings.Repeat("A", 76), strings.Repeat("B", 75)+"="
	unterminated := "-----BEGIN RSA PRIVATE KEY-----\n" + b1 + "\n" + b2
	t.Run("unterminated, then prose", func(t *testing.T) {
		note := "Note: I cut it off here, what is wrong?"
		if got, want := Redact(unterminated+"\n"+note), "[REDACTED]\n"+note; got != want {
			t.Errorf("Redact = %q, want %q", got, want)
		}
	})

	// A BEGIN line inside a replaced block is not looked at again.
	t.Run("truncated block, then a full one", func(t *testing.T) {
		begin, end := "-----BEGIN RSA PRIVATE KEY-----", "-----END RSA PRIVATE KEY-----"
		a := strings.Repeat("A", 64)
		got := Redact(begin + "\n" + a + "\n" + begin + "\n" + a + "\n" + end + " after")
		if want := "[REDACTED] after"; got != want {
			t.Errorf("Redact = %q, want %q", got, want)
		}
	})

	// The END line must start within 16384 characters of the BEGIN line's end,
	// counted in characters, not bytes. Past that the block is unterminated
	// and only the base64 after BEGIN goes.
	t.Run("END line at the scan limit", func(t *testing.T) {
		begin, end := "-----BEGIN RSA PRIVATE KEY-----", "-----END RSA PRIVATE KEY-----"
		for _, c := range []struct {
			name, filler string
		}{{"ascii", "."}, {"multibyte", "\u00e9"}} {
			within := begin + strings.Repeat(c.filler, keyScanRunes) + end + " after"
			if got, want := Redact(within), "[REDACTED] after"; got != want {
				t.Errorf("%s, END starting at the limit: Redact = %q, want %q", c.name, got, want)
			}
			beyond := begin + strings.Repeat(c.filler, keyScanRunes+1) + end + " after"
			if got, want := Redact(beyond), "[REDACTED]"+strings.Repeat(c.filler, keyScanRunes+1)+end+" after"; got != want {
				t.Errorf("%s, END one past the limit: got %d bytes, want the text after the BEGIN line kept", c.name, len(got))
			}
		}
	})
}

func TestRedactKeepsPublicKeysAndCertificates(t *testing.T) {
	for _, label := range []string{"PUBLIC KEY", "CERTIFICATE"} {
		t.Run(label, func(t *testing.T) {
			in := "is this ok: " + pem(label, "", keyBody) + " thanks"
			if got := Redact(in); got != in {
				t.Errorf("Redact(%q) = %q, want it unchanged", in, got)
			}
		})
	}
}

// Python's \s and \w match every Unicode space and letter; Go's match ASCII
// only. These pin that the Go patterns still redact what the plugin redacts.
func TestRedactMatchesPythonUnicodeClasses(t *testing.T) {
	begin := "-----BEGIN RSA PRIVATE KEY-----"
	for _, c := range []struct{ name, in, want string }{
		{"em space after a colon", "password:\u2003hunter2hunter2", "[REDACTED]"},
		{"no-break space after a colon", "password:\u00a0hunter2hunter2", "[REDACTED]"},
		{"no-break space after Bearer", "Bearer\u00a0abcdefghijklmnop", "[REDACTED]"},
		{"letter outside ASCII in a bearer token", "Bearer abcd\u00e9fghij", "[REDACTED]"},
		{"em space after BEGIN", begin + "\u2003" + strings.Repeat("A", 64) + " tail", "[REDACTED] tail"},
		{"em space in a URL password", "postgres://app:pa\u2003ss@h/", "postgres://app:pa\u2003ss@h/"},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := Redact(c.in); got != c.want {
				t.Errorf("Redact(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

// Accepted difference (a) from the comment in redact.go: \b is ASCII in Go, so
// where a non-ASCII letter sits right before sk- or a URL scheme Go redacts
// and the plugin, whose \b is Unicode, leaves the text alone. Redacting more
// is the safe direction.
func TestRedactASCIIWordBoundaryRedactsMore(t *testing.T) {
	for _, c := range []struct{ name, in, want string }{
		{"sk- key after a letter", "x\u00e9sk-ant-" + strings.Repeat("a", 20), "x\u00e9[REDACTED]"},
		{"URL after a letter", "\u00e9postgres://app:S3cretPassw0rd@db/x", "\u00e9postgres://app:[REDACTED]@db/x"},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := Redact(c.in); got != c.want {
				t.Errorf("Redact(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}
