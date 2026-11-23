package releases

import (
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
)

// loopbackOnly fails any request that would leave the machine, so a test that
// forgets to point at a fake server fails loudly instead of calling Kemory.
type loopbackOnly struct{ next http.RoundTripper }

func (l loopbackOnly) RoundTrip(r *http.Request) (*http.Response, error) {
	host := r.URL.Hostname()
	if ip := net.ParseIP(host); (ip == nil || !ip.IsLoopback()) && host != "localhost" {
		panic("test tried to reach " + r.URL.String())
	}
	return l.next.RoundTrip(r)
}

func TestMain(m *testing.M) {
	guard := loopbackOnly{next: http.DefaultTransport}
	for _, c := range []*http.Client{HTTPClient} {
		c.Transport = guard
	}
	os.Exit(m.Run())
}

func TestTheLoopbackGuardRefusesRealHosts(t *testing.T) {
	clients := []struct {
		name   string
		client *http.Client
	}{
		{"releases.HTTPClient", HTTPClient},
	}
	for _, tc := range clients {
		t.Run(tc.name, func(t *testing.T) {
			if _, ok := tc.client.Transport.(loopbackOnly); !ok {
				t.Fatalf("%s.Transport is %T, want loopbackOnly", tc.name, tc.client.Transport)
			}
			defer func() {
				if s, _ := recover().(string); !strings.Contains(s, "guard-check.invalid") {
					t.Fatalf("guard did not fire: %q", s)
				}
			}()
			_, _ = tc.client.Get("https://guard-check.invalid/")
		})
	}
}
