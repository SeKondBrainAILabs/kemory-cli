package auth

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/SeKondBrainAILabs/kemory-cli/internal/exits"
)

// An issuer that is up but failing (5xx) has not rejected the refresh token.
// Like an unreachable issuer, it must not be reported as "not logged in":
// `kemory login` cannot fix an issuer outage.
func TestIssuerServerErrorOnRefreshIsNotReportedAsLoggedOut(t *testing.T) {
	for _, code := range []int{http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			setHome(t)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(code)
				_, _ = w.Write([]byte("upstream unavailable"))
			}))
			defer srv.Close()
			saveCreds(t, srv.URL, 0)
			c, err := ValidCredentials("prod", false)
			if c == nil && err == nil {
				t.Fatalf("issuer %d: ValidCredentials returned (nil, nil), which callers report as \"not logged in\"", code)
			}
			var flow *FlowError
			if errors.As(err, &flow) {
				t.Fatalf("issuer %d: got a FlowError, want a network error: %v", code, err)
			}
			if got := exits.Code(err); got != exits.Network {
				t.Fatalf("issuer %d: exit code = %d, want %d", code, got, exits.Network)
			}
			if !strings.Contains(err.Error(), srv.URL) || !strings.Contains(err.Error(), strconv.Itoa(code)) {
				t.Fatalf("message %q should name the issuer and status", err)
			}
			if strings.Contains(err.Error(), "kemory login") {
				t.Fatalf("message %q must not tell the user to log in", err)
			}
		})
	}
}
