package cli

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestStyledHelpListsGroupsCommandsAndFlags(t *testing.T) {
	root := newRoot()
	h := styledHelp(root)
	for _, want := range []string{"persistent memory for your AI agents", "Usage", "Getting started", "connect", "Your vault", "Flags", "--output", "Run kemory <command> --help for details."} {
		if !strings.Contains(h, want) {
			t.Fatalf("missing %q in:\n%s", want, h)
		}
	}
	doctor, _, err := root.Find([]string{"doctor"})
	if err != nil {
		t.Fatal(err)
	}
	h = styledHelp(doctor)
	for _, want := range []string{"kemory doctor", "Global flags", "--fix"} {
		if !strings.Contains(h, want) {
			t.Fatalf("missing %q in:\n%s", want, h)
		}
	}
}

func TestHelpIsPlainOutsideATerminal(t *testing.T) {
	setHome(t)
	code, out, _ := run(t, "--help")
	if code != 0 || !strings.Contains(out, "Usage:") || strings.Contains(out, "\x1b[") {
		t.Fatalf("plain help expected, got %d:\n%s", code, out)
	}
}

func TestWrapKeepsWordsWithinWidth(t *testing.T) {
	for _, l := range wrap(strings.Repeat("word ", 30), 22) {
		if len(l) > 22 {
			t.Fatalf("line too long: %q", l)
		}
	}
	_ = cobra.Command{}
}
