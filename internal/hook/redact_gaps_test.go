package hook

import (
	"strings"
	"testing"
)

// Each value below is a fake credential in a shape users paste into coding
// sessions every day. Capture stores Redact's output on the server, so none
// of the secret part may survive. The values are built at run time so the
// source holds no credential-shaped literal.
func TestRedactRemovesCommonCredentialShapes(t *testing.T) {
	fill := strings.Repeat("Ab3Xy9Qz", 6) // 48 characters
	tail := strings.Repeat("x9Y", 20)
	cases := []struct{ name, in, secret string }{
		{"json api key", `{"api_key": "` + fill + `"}`, fill},
		{"json password", `{"password": "` + fill + `"}`, fill},
		{"yaml quoted key", `api_key: "` + fill + `"`, fill},
		{"anthropic key", "key " + "sk-" + "ant-api03-" + fill, fill},
		{"openai project key", "key " + "sk-" + "proj-" + fill, fill},
		{"github fine-grained pat", "github" + "_pat_" + fill[:22] + "_" + tail, tail},
		{"aws secret access key", "AWS_SECRET_ACCESS_KEY=" + fill, fill},
		{"private key body", "-----BEGIN RSA PRIVATE KEY-----\n" + fill + "\n-----END RSA PRIVATE KEY-----", fill},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Redact(c.in); strings.Contains(got, c.secret) {
				t.Errorf("secret survived redaction: Redact(%q) = %q", c.in, got)
			}
		})
	}
}
