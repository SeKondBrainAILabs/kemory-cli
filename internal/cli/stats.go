package cli

import (
	"fmt"
	"net/url"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"github.com/SeKondBrainAILabs/kemory-cli/internal/api"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/exits"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/render"
)

// These aggregate over a user's whole recall history.
const statsTimeout = 30 * time.Second

// Recall cost and usage, with three rules the output keeps:
//   - It says "cost", never "saved": Kemory sees the turn with recall and has
//     no counterfactual to subtract from.
//   - The window is printed next to every claim. Telemetry is pruned, so "not
//     recalled" means "not in the last N days", never "never".
//   - Ratings are opinions recorded only when an agent rates a memory; no
//     rating means no opinion, not "fine".

func ratedCell(up, down int) string {
	switch {
	case up > 0 && down > 0:
		return fmt.Sprintf("%d↑ %d↓", up, down)
	case up > 0:
		return fmt.Sprintf("%d↑", up)
	case down > 0:
		return fmt.Sprintf("%d↓", down)
	}
	return "-"
}

func checkDays(days int) error {
	if days < 1 || days > 365 {
		return exits.New("--days must be between 1 and 365", exits.Usage)
	}
	return nil
}

func newStatsCmd(g *globals) *cobra.Command {
	var days int
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "stats",
		Short: "How much your memory is used, and what it costs.",
		Long: "How much your memory is actually used, and what it costs in context tokens.\n\n" +
			"  kemory stats\n  kemory stats memories --never-recalled",
		Args: cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			if err := checkDays(days); err != nil {
				return err
			}
			creds, err := api.RequireCreds(g.env)
			if err != nil {
				return err
			}
			path := "/api/v1/analytics/recall/summary?days=" + strconv.Itoa(days)
			resp, err := api.Request(creds, "GET", path, nil, statsTimeout)
			if err != nil {
				return err
			}
			if resp.Status != 200 {
				return api.Fail(resp, "GET "+path)
			}
			if wantsJSON(asJSON) {
				return emitRaw(resp.Body)
			}
			var d struct {
				WindowDays       int    `json:"window_days"`
				Recalls          int    `json:"recalls"`
				EmptyRecalls     int    `json:"empty_recalls"`
				FirstRecordedAt  string `json:"first_recorded_at"`
				DistinctMemories int    `json:"distinct_memories_recalled"`
				MatchedRecalls   int    `json:"matched_recalls"`
				FlatRecalls      int    `json:"flat_recalls"`
				TokensInjected   int    `json:"tokens_injected"`
				MeanTokens       any    `json:"mean_tokens_per_recall"`
				ByNamespace      []struct {
					Namespace      string `json:"namespace"`
					Recalls        int    `json:"recalls"`
					TokensInjected int    `json:"tokens_injected"`
				} `json:"by_namespace"`
				ByEntryPoint []struct {
					EntryPoint string `json:"entry_point"`
					Recalls    int    `json:"recalls"`
				} `json:"by_entry_point"`
			}
			if err := resp.JSON(&d); err != nil {
				return err
			}
			window := d.WindowDays
			if window == 0 {
				window = days
			}
			if d.Recalls == 0 {
				render.Echo(fmt.Sprintf("No recalls recorded in the last %d days.", window))
				render.Muted("That means nothing queried your memory — not that your memories are unhelpful. " +
					"Recall telemetry only starts at the version that records it.")
				return nil
			}
			label := fmt.Sprintf("%d days", window)
			if since := datePart(d.FirstRecordedAt); since != "" {
				// What was asked for, and what was actually observed.
				label += " (recording since " + since + ")"
			}
			mean := "-"
			if d.MeanTokens != nil {
				mean = fmt.Sprint(d.MeanTokens)
			}
			render.KVs([]render.KV{
				{Key: "window", Value: label},
				{Key: "recalls", Value: strconv.Itoa(d.Recalls)},
				{Key: "returned nothing", Value: fmt.Sprintf("%d (%d%%)", d.EmptyRecalls, d.EmptyRecalls*100/d.Recalls)},
				{Key: "memories returned", Value: strconv.Itoa(d.DistinctMemories)},
				{Key: "found a real match", Value: strconv.Itoa(d.MatchedRecalls)},
				{Key: "flat (nothing stood out)", Value: strconv.Itoa(d.FlatRecalls)},
				{Key: "tokens injected (cost)", Value: thousands(d.TokensInjected)},
				{Key: "mean tokens / recall", Value: mean},
			})
			if len(d.ByNamespace) > 0 {
				rows := make([][]string, 0, len(d.ByNamespace))
				for _, n := range d.ByNamespace {
					rows = append(rows, []string{orDefault(n.Namespace, "(none)"), strconv.Itoa(n.Recalls), thousands(n.TokensInjected)})
				}
				render.Echo("")
				render.Table(fmt.Sprintf("By namespace · %dd", window), []string{"NAMESPACE", "RECALLS", "TOKENS"}, rows)
			}
			if len(d.ByEntryPoint) > 0 {
				rows := make([][]string, 0, len(d.ByEntryPoint))
				for _, e := range d.ByEntryPoint {
					rows = append(rows, []string{orDefault(e.EntryPoint, "(unknown)"), strconv.Itoa(e.Recalls)})
				}
				render.Echo("")
				render.Table(fmt.Sprintf("By caller · %dd", window), []string{"CALLER", "RECALLS"}, rows)
			}
			return nil
		},
	}
	cmd.Flags().IntVar(&days, "days", 30, "Window to report on (1-365).")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Print the raw response.")
	cmd.AddCommand(newStatsMemoriesCmd(g))
	return cmd
}

func thousands(n int) string {
	s := strconv.Itoa(n)
	if n < 0 {
		return "-" + thousands(-n)
	}
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

type statsMemoryRow struct {
	MemoryID         string `json:"memory_id"`
	Namespace        string `json:"namespace"`
	Recalls          int    `json:"recalls"`
	LastRecalledAt   string `json:"last_recalled_at"`
	AgeDays          any    `json:"age_days"`
	UpRatings        int    `json:"up_ratings"`
	DownRatings      int    `json:"down_ratings"`
	Preview          string `json:"preview"`
	LastRatingReason string `json:"last_rating_reason"`
}

func newStatsMemoriesCmd(g *globals) *cobra.Command {
	var days, limit int
	var neverRecalled, ratedDown, asJSON bool
	var sortBy string
	cmd := &cobra.Command{
		Use:   "memories",
		Short: "Which memories get recalled, which never do, or which were rated down.",
		Long: "  kemory stats memories                    # most-recalled first\n" +
			"  kemory stats memories --never-recalled   # the review queue, oldest first\n" +
			"  kemory stats memories --rated-down       # memories someone judged wrong\n" +
			"  kemory stats memories --sort rating      # best-judged first\n\n" +
			"--never-recalled means \"not recalled in the last N days\", never \"never\".\n" +
			"--rated-down ignores --days: ratings are never pruned. --sort rating skips\n" +
			"unrated memories rather than scoring them zero.",
		Args: cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			if err := checkDays(days); err != nil {
				return err
			}
			if limit < 1 || limit > 200 {
				return exits.New("--limit must be between 1 and 200", exits.Usage)
			}
			switch {
			case neverRecalled && ratedDown:
				return exits.New("--never-recalled and --rated-down are two different queues; pass one.", exits.Usage)
			case sortBy == "recalls" && neverRecalled:
				return exits.New("--sort recalls is empty under --never-recalled: every row has zero recalls.", exits.Usage)
			case sortBy != "" && ratedDown:
				return exits.New("--rated-down is already a rating view, ordered by most-recently-rated; --sort does not apply.", exits.Usage)
			}
			if sortBy != "" {
				if err := oneOf(sortBy, []string{"recalls", "rating", "age"}, "--sort"); err != nil {
					return err
				}
			}
			effective := sortBy
			if effective == "" {
				effective = "recalls"
				if neverRecalled {
					effective = "age"
				}
			}
			creds, err := api.RequireCreds(g.env)
			if err != nil {
				return err
			}
			q := url.Values{
				"days": {strconv.Itoa(days)}, "limit": {strconv.Itoa(limit)}, "sort": {effective},
				"never_recalled": {strconv.FormatBool(neverRecalled)}, "rated_down": {strconv.FormatBool(ratedDown)},
			}
			path := "/api/v1/analytics/recall/memories?" + q.Encode()
			resp, err := api.Request(creds, "GET", path, nil, statsTimeout)
			if err != nil {
				return err
			}
			if resp.Status != 200 {
				return api.Fail(resp, "GET /api/v1/analytics/recall/memories")
			}
			if wantsJSON(asJSON) {
				return emitRaw(resp.Body)
			}
			var d struct {
				WindowDays       int              `json:"window_days"`
				TotalMemories    int              `json:"total_memories"`
				NeverRecalled    int              `json:"never_recalled"`
				RatedDownTotal   int              `json:"rated_down_total"`
				RatedTotal       int              `json:"rated_total"`
				FirstRecordedAt  string           `json:"first_recorded_at"`
				CoversFullWindow *bool            `json:"covers_full_window"`
				Rows             []statsMemoryRow `json:"rows"`
			}
			if err := resp.JSON(&d); err != nil {
				return err
			}
			if d.WindowDays == 0 {
				d.WindowDays = days
			}
			byRating := effective == "rating"
			order := map[string]string{"recalls": "most recalled first", "rating": "best rated first", "age": "oldest first"}[effective]
			switch {
			case ratedDown:
				renderRatedDown(d.RatedDownTotal, d.Rows)
			case neverRecalled:
				covers := d.CoversFullWindow == nil || *d.CoversFullWindow
				renderNeverRecalled(d.WindowDays, d.TotalMemories, d.NeverRecalled, d.RatedTotal, d.FirstRecordedAt, covers, byRating, order, d.Rows)
			default:
				renderRecalled(d.WindowDays, d.TotalMemories, d.NeverRecalled, d.RatedTotal, d.RatedDownTotal, byRating, order, d.Rows)
			}
			return nil
		},
	}
	f := cmd.Flags()
	f.IntVar(&days, "days", 30, "Window to report on (1-365).")
	f.BoolVar(&neverRecalled, "never-recalled", false, "List memories NOT recalled in the window, oldest first.")
	f.BoolVar(&ratedDown, "rated-down", false, "List memories whose latest rating is down. Not bound to --days.")
	f.StringVar(&sortBy, "sort", "", "Order rows by recalls, rating or age. `rating` skips unrated memories.")
	f.IntVar(&limit, "limit", 20, "Rows to show (1-200).")
	f.BoolVar(&asJSON, "json", false, "Print the raw response.")
	return cmd
}

func renderRatedDown(total int, rows []statsMemoryRow) {
	if len(rows) == 0 {
		render.Echo("Nothing rated down. Either nothing has been rated at all, or every rated memory held up.")
		render.Muted("Rating is opt-in — an agent has to call `kemory_rate_memory` after using a memory. " +
			"No rating here means no opinion exists, not that everything passed.")
		return
	}
	render.Echo(fmt.Sprintf("%d memories are currently rated down (latest verdict, not windowed).", total))
	render.Echo("")
	table := make([][]string, 0, len(rows))
	for _, r := range rows {
		table = append(table, []string{r.MemoryID, orDefault(r.Namespace, "-"), orDefault(r.LastRatingReason, "-"), orDefault(r.Preview, "-")})
	}
	render.Table("Rated down · most recently rated first", []string{"MEMORY ID", "NAMESPACE", "REASON", "PREVIEW"}, table)
	render.Echo("")
	render.Muted("Only the LATEST rating counts — re-rating a memory 'up' removes it from this list " +
		"even though the down-rating still happened. Read one before deleting it.")
}

func renderNeverRecalled(window, total, never, ratedTotal int, firstRecorded string, covers, byRating bool, order string, rows []statsMemoryRow) {
	since := datePart(firstRecorded)
	// Until telemetry covers the window, "not recalled in N days" accuses
	// every memory at once. The count is withheld rather than footnoted.
	if !covers {
		start := "when the recording version deploys"
		if since != "" {
			start = "on " + since
		}
		render.Echo(fmt.Sprintf("Not enough history yet: recall telemetry starts %s, which is inside the %d-day window.", start, window))
		when := "Come back once the window is covered"
		if t, err := time.Parse("2006-01-02", since); err == nil {
			when = "This window is fully covered on " + t.AddDate(0, 0, window).Format("2006-01-02")
		}
		render.Muted(fmt.Sprintf("%d of %d memories have no recall recorded, but that is a statement about how long this "+
			"has been measured, not about the memories. %s, or narrow it with --days.", never, total, when))
		return
	}
	share := "-"
	if total > 0 {
		share = fmt.Sprintf("%d%%", never*100/total)
	}
	render.Echo(fmt.Sprintf("%d of %d memories (%s) were not recalled in the last %d days.", never, total, share, window))
	if len(rows) == 0 {
		if byRating {
			render.Muted(fmt.Sprintf("None of them carry a rating, so a rating sort has nothing to rank. %d of %d memories have been rated at all.", ratedTotal, total))
			return
		}
		render.Muted("Nothing to review — every memory was recalled at least once in the window.")
		return
	}
	render.Echo("")
	if byRating {
		render.Muted(fmt.Sprintf("Ranking only the rated ones: %d shown, %d of %d memories carry any rating. Unrated memories are skipped, not scored zero.", len(rows), ratedTotal, total))
		render.Echo("")
	}
	table := make([][]string, 0, len(rows))
	for _, r := range rows {
		age := "-"
		if r.AgeDays != nil {
			age = fmt.Sprint(r.AgeDays)
		}
		table = append(table, []string{r.MemoryID, orDefault(r.Namespace, "-"), age, ratedCell(r.UpRatings, r.DownRatings), orDefault(r.Preview, "-")})
	}
	render.Table(fmt.Sprintf("Never recalled in %dd · %s", window, order), []string{"MEMORY ID", "NAMESPACE", "AGE", "RATED", "PREVIEW"}, table)
	render.Echo("")
	if !byRating {
		render.Muted("Oldest first, because age is what makes this actionable: a memory stored yesterday has not had its chance yet.")
	}
	render.Muted("Read one before deleting it — never recalled is not the same as not worth keeping.")
}

func renderRecalled(window, total, never, ratedTotal, ratedDownTotal int, byRating bool, order string, rows []statsMemoryRow) {
	if len(rows) == 0 {
		if byRating {
			render.Echo("No recalled memory carries a rating, so there is nothing to rank.")
			render.Muted(fmt.Sprintf("%d of %d memories have been rated at all. Rating is opt-in — an agent has to call `kemory_rate_memory` after using a memory.", ratedTotal, total))
			return
		}
		render.Echo(fmt.Sprintf("No memories were recalled in the last %d days.", window))
		return
	}
	if byRating {
		render.Muted(fmt.Sprintf("Ranking only the rated ones: %d shown, %d of %d memories carry any rating. Unrated memories are skipped, not scored zero.", len(rows), ratedTotal, total))
		render.Echo("")
	}
	table := make([][]string, 0, len(rows))
	for _, r := range rows {
		table = append(table, []string{r.MemoryID, orDefault(r.Namespace, "-"), strconv.Itoa(r.Recalls), orDefault(datePart(r.LastRecalledAt), "-"), ratedCell(r.UpRatings, r.DownRatings), orDefault(r.Preview, "-")})
	}
	render.Table(fmt.Sprintf("Recalled in %dd · %s", window, order), []string{"MEMORY ID", "NAMESPACE", "RECALLS", "LAST", "RATED", "PREVIEW"}, table)
	render.Echo("")
	render.Muted(fmt.Sprintf("%d of %d memories were not recalled at all in this window — see `kemory stats memories --never-recalled`.", never, total))
	render.Muted("Recalled is not the same as USED: kemory sees what it returned, not what helped.")
	if ratedDownTotal > 0 {
		render.Muted(fmt.Sprintf("%d memories are currently rated down — see `kemory stats memories --rated-down`.", ratedDownTotal))
	}
}
