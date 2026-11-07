package cli

import (
	"testing"

	"github.com/SeKondBrainAILabs/kemory-cli/internal/hosts"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/ui"
)

func TestRestartStepNamesTheAppAndSkipsWarp(t *testing.T) {
	for _, host := range hosts.Names {
		want := "restart " + ui.AppName(host)
		if host == "warp" {
			want = ""
		}
		if got := restartStep(host); got != want {
			t.Errorf("restartStep(%q) = %q, want %q", host, got, want)
		}
	}
	if got := restartStep("vscode"); got != "restart VS Code" {
		t.Errorf("restartStep(vscode) = %q", got)
	}
}
