package cli

import (
	"net"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/SeKondBrainAILabs/kemory-cli/internal/api"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/auth"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/releases"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/residency"
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
	for _, c := range []*http.Client{api.HTTPClient, auth.HTTPClient, residency.HTTPClient, releases.HTTPClient, HealthClient} {
		c.Transport = guard
	}
	os.Exit(m.Run())
}

func TestTheLoopbackGuardRefusesRealHosts(t *testing.T) {
	defer func() {
		if r := recover(); r == nil || !strings.Contains(r.(string), "api.kemory.s9n.ai") {
			t.Fatalf("guard did not fire: %v", r)
		}
	}()
	req, _ := http.NewRequest(http.MethodGet, "https://api.kemory.s9n.ai/health/live", nil)
	_, _ = loopbackOnly{next: http.DefaultTransport}.RoundTrip(req)
}
