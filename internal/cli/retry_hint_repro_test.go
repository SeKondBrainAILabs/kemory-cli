package cli

import (
	"net/http"
	"strings"
	"testing"

	"github.com/SeKondBrainAILabs/kemory-cli/internal/exits"
)

func TestRecallOutageRetryHintReadsWithoutRetryAfter(t *testing.T) {
	setHome(t)
	newFakeVault(t, map[string]func(http.ResponseWriter){
		searchPath: func(w http.ResponseWriter) { w.WriteHeader(http.StatusServiceUnavailable) },
	})
	code, _, errOut := run(t, "recall", "x")
	if code != exits.Unavailable {
		t.Fatalf("got exit %d, want %d", code, exits.Unavailable)
	}
	if strings.Contains(errOut, "a fews") || !strings.Contains(errOut, "Retry in a few seconds") {
		t.Fatalf("garbled retry hint: %q", errOut)
	}
}
