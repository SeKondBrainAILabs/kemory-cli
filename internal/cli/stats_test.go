package cli

import (
	"net/http"
	"strings"
	"testing"
)

const statsMemoriesPath = "/api/v1/analytics/recall/memories"

// statsMemories serves body on the memories endpoint and runs `kemory stats
// memories` with args. -q is never passed: it would drop the muted lines.
func statsMemories(t *testing.T, body string, args ...string) string {
	t.Helper()
	setHome(t)
	newFakeVault(t, map[string]func(http.ResponseWriter){statsMemoriesPath: jsonReply(200, body)})
	code, out, errOut := run(t, append([]string{"stats", "memories"}, args...)...)
	if code != 0 {
		t.Fatalf("exit %d\n%s\n%s", code, out, errOut)
	}
	// stats.go says "cost", never "saved": there is no counterfactual to subtract.
	if strings.Contains(strings.ToLower(out), "saved") {
		t.Fatalf("stats must never claim savings:\n%s", out)
	}
	return out
}

func wantAll(t *testing.T, out string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func wantNone(t *testing.T, out string, unwanted ...string) {
	t.Helper()
	for _, s := range unwanted {
		if strings.Contains(out, s) {
			t.Errorf("unexpected %q in:\n%s", s, out)
		}
	}
}

func TestStatsMemoriesDefaultListsRecalledRows(t *testing.T) {
	const rows = `"window_days": 30, "total_memories": 10, "never_recalled": 4, "rows": [
		{"memory_id": "mem-aaa", "namespace": "project:kemory", "recalls": 7, "last_recalled_at": "2026-11-02T08:30:00Z",
		 "up_ratings": 2, "down_ratings": 1, "preview": "Use the fake vault"},
		{"memory_id": "mem-bbb", "namespace": "", "recalls": 3, "last_recalled_at": "2026-10-20T00:00:00Z", "preview": ""}]`
	common := []string{
		"Recalled in 30d · most recalled first", "MEMORY ID", "RECALLS", "LAST", "RATED",
		"mem-aaa", "project:kemory", "2026-11-02", "2↑ 1↓", "Use the fake vault",
		"mem-bbb", "2026-10-20",
		"4 of 10 memories were not recalled at all in this window",
		"Recalled is not the same as USED",
	}
	t.Run("rated down total shows the pointer", func(t *testing.T) {
		out := statsMemories(t, `{"rated_down_total": 3, `+rows+`}`)
		wantAll(t, out, append(common, "3 memories are currently rated down — see `kemory stats memories --rated-down`")...)
		wantNone(t, out, "2026-11-02T08")
		// The second row has no namespace, ratings or preview: each cell falls back to "-".
		for _, line := range strings.Split(out, "\n") {
			if strings.Contains(line, "mem-bbb") {
				if got := strings.Fields(line); strings.Join(got, " ") != "mem-bbb - 3 2026-10-20 - -" {
					t.Errorf("row cells: %q", line)
				}
			}
		}
	})
	t.Run("rated down total zero hides the pointer", func(t *testing.T) {
		out := statsMemories(t, `{"rated_down_total": 0, `+rows+`}`)
		wantAll(t, out, common...)
		wantNone(t, out, "currently rated down")
	})
}

func TestStatsMemoriesDefaultWithNoRowsSaysNothingWasRecalled(t *testing.T) {
	t.Run("window from the response", func(t *testing.T) {
		out := statsMemories(t, `{"window_days": 30, "total_memories": 10, "never_recalled": 10, "rows": []}`)
		wantAll(t, out, "No memories were recalled in the last 30 days.")
		wantNone(t, out, "MEMORY ID")
	})
	t.Run("window from --days when the response omits it", func(t *testing.T) {
		out := statsMemories(t, `{"rows": []}`, "--days", "7")
		wantAll(t, out, "No memories were recalled in the last 7 days.")
	})
}

func TestStatsMemoriesSortRatingSkipsUnratedMemories(t *testing.T) {
	out := statsMemories(t, `{"window_days": 30, "total_memories": 10, "never_recalled": 4, "rated_total": 5, "rows": [
		{"memory_id": "mem-top", "recalls": 2, "last_recalled_at": "2026-11-01T00:00:00Z", "up_ratings": 4}]}`, "--sort", "rating")
	wantAll(t, out,
		"Recalled in 30d · best rated first", "mem-top", "4↑",
		"Ranking only the rated ones: 1 shown, 5 of 10 memories carry any rating. Unrated memories are skipped, not scored zero.")
	wantNone(t, out, "most recalled first")
}

func TestStatsMemoriesSortRatingWithNoRowsSaysNothingToRank(t *testing.T) {
	out := statsMemories(t, `{"window_days": 30, "total_memories": 10, "rated_total": 2, "rows": []}`, "--sort", "rating")
	wantAll(t, out, "No recalled memory carries a rating, so there is nothing to rank.", "2 of 10 memories have been rated at all")
	wantNone(t, out, "No memories were recalled")
}

func TestStatsMemoriesRatedDownListsLatestDownRatings(t *testing.T) {
	out := statsMemories(t, `{"rated_down_total": 2, "rows": [
		{"memory_id": "mem-bad", "namespace": "project:kemory", "last_rating_reason": "outdated advice", "preview": "Run make old"},
		{"memory_id": "mem-meh", "namespace": "", "last_rating_reason": "", "preview": ""}]}`, "--rated-down")
	wantAll(t, out,
		"2 memories are currently rated down (latest verdict, not windowed).",
		"Rated down · most recently rated first", "MEMORY ID", "NAMESPACE", "REASON", "PREVIEW",
		"Only the LATEST rating counts")
	// Columns are MEMORY ID, NAMESPACE, REASON, PREVIEW; empty cells fall back to "-".
	for id, want := range map[string]string{
		"mem-bad": "mem-bad project:kemory outdated advice Run make old",
		"mem-meh": "mem-meh - - -",
	} {
		found := false
		for _, line := range strings.Split(out, "\n") {
			if strings.Contains(line, id) {
				found = true
				if got := strings.Join(strings.Fields(line), " "); got != want {
					t.Errorf("row %s: got %q, want %q", id, got, want)
				}
			}
		}
		if !found {
			t.Errorf("missing row %s in:\n%s", id, out)
		}
	}
}

func TestStatsMemoriesRatedDownWithNoRowsSaysNoOpinion(t *testing.T) {
	out := statsMemories(t, `{"rated_down_total": 0, "rows": []}`, "--rated-down")
	wantAll(t, out, "Nothing rated down.", "No rating here means no opinion exists, not that everything passed.")
	wantNone(t, out, "Only the LATEST rating counts")
}

func TestStatsMemoriesNeverRecalledCoveredWindow(t *testing.T) {
	const head = `"window_days": 30, "total_memories": 8, "never_recalled": 3, "rated_total": 1, "covers_full_window": true`
	t.Run("rows list oldest first", func(t *testing.T) {
		out := statsMemories(t, `{`+head+`, "rows": [
			{"memory_id": "mem-old", "namespace": "project:kemory", "age_days": 90, "down_ratings": 3, "preview": "Old note"},
			{"memory_id": "mem-new", "age_days": null}]}`, "--never-recalled")
		wantAll(t, out,
			"3 of 8 memories (37%) were not recalled in the last 30 days.",
			"Never recalled in 30d · oldest first", "MEMORY ID", "AGE", "RATED",
			"mem-old", "90", "3↓", "Old note", "mem-new",
			"Oldest first, because age is what makes this actionable")
		for _, line := range strings.Split(out, "\n") {
			if strings.Contains(line, "mem-new") {
				if got := strings.Join(strings.Fields(line), " "); got != "mem-new - - - -" {
					t.Errorf("a null age_days must render as '-': %q", line)
				}
			}
		}
	})
	t.Run("no rows", func(t *testing.T) {
		out := statsMemories(t, `{`+head+`, "never_recalled": 0, "rows": []}`, "--never-recalled")
		wantAll(t, out,
			"0 of 8 memories (0%) were not recalled in the last 30 days.",
			"Nothing to review — every memory was recalled at least once in the window.")
	})
	t.Run("no memories at all has no percentage", func(t *testing.T) {
		out := statsMemories(t, `{"window_days": 30, "total_memories": 0, "never_recalled": 0, "rows": []}`, "--never-recalled")
		wantAll(t, out, "0 of 0 memories (-) were not recalled in the last 30 days.")
	})
	t.Run("sort rating with rows", func(t *testing.T) {
		out := statsMemories(t, `{`+head+`, "rows": [{"memory_id": "mem-rated", "age_days": 12, "up_ratings": 1}]}`,
			"--never-recalled", "--sort", "rating")
		wantAll(t, out,
			"3 of 8 memories (37%) were not recalled in the last 30 days.",
			"Never recalled in 30d · best rated first", "mem-rated",
			"Ranking only the rated ones: 1 shown, 1 of 8 memories carry any rating.")
		wantNone(t, out, "Oldest first, because")
	})
	t.Run("sort rating with no rows", func(t *testing.T) {
		out := statsMemories(t, `{`+head+`, "rows": []}`, "--never-recalled", "--sort", "rating")
		wantAll(t, out,
			"3 of 8 memories (37%) were not recalled in the last 30 days.",
			"None of them carry a rating", "1 of 8 memories have been rated at all")
		wantNone(t, out, "Nothing to review")
	})
}

func TestRatedCell(t *testing.T) {
	for _, c := range []struct {
		up, down int
		want     string
	}{
		{0, 0, "-"},
		{2, 0, "2↑"},
		{0, 3, "3↓"},
		{2, 3, "2↑ 3↓"},
	} {
		if got := ratedCell(c.up, c.down); got != c.want {
			t.Errorf("ratedCell(%d, %d) = %q, want %q", c.up, c.down, got, c.want)
		}
	}
}

func TestStatsSummaryByCallerAndJSON(t *testing.T) {
	const body = `{"window_days": 30, "recalls": 5, "empty_recalls": 0, "mean_tokens_per_recall": 120,
		"by_entry_point": [{"entry_point": "mcp", "recalls": 4}, {"entry_point": "", "recalls": 1}]}`
	setHome(t)
	newFakeVault(t, map[string]func(http.ResponseWriter){"/api/v1/analytics/recall/summary": jsonReply(200, body)})
	code, out, errOut := run(t, "stats")
	if code != 0 {
		t.Fatalf("exit %d\n%s\n%s", code, out, errOut)
	}
	wantAll(t, out, "By caller · 30d", "CALLER", "mcp", "(unknown)", "mean tokens / recall", "120")
	wantNone(t, strings.ToLower(out), "saved")

	code, out, _ = run(t, "stats", "--json")
	if code != 0 || !strings.Contains(out, `"by_entry_point"`) || strings.Contains(out, "By caller") {
		t.Fatalf("--json must print the raw response, got %d:\n%s", code, out)
	}
}

func TestStatsMemoriesServerErrorFails(t *testing.T) {
	setHome(t)
	newFakeVault(t, map[string]func(http.ResponseWriter){statsMemoriesPath: jsonReply(500, `{"detail": "boom"}`)})
	if code, out, _ := run(t, "stats", "memories"); code == 0 {
		t.Fatalf("a 500 must fail the command:\n%s", out)
	}
}
