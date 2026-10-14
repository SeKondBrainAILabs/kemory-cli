package cli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/SeKondBrainAILabs/kemory-cli/internal/api"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/config"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/exits"
	"github.com/SeKondBrainAILabs/kemory-cli/internal/render"
)

// Stdin is where piped content and confirmations are read from.
// Overridable in tests.
var Stdin io.Reader = os.Stdin

// stdinIsTerminal reports whether Stdin is an interactive terminal.
var stdinIsTerminal = func() bool {
	f, ok := Stdin.(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}

var (
	visibilities = []string{"agent-private", "user-private", "team", "org-public"}
	searchModes  = []string{"hybrid", "fts", "dense"}
)

const (
	searchTimeout = 60 * time.Second  // a vector pass and, for encrypted vaults, a decrypt pass
	askTimeout    = 120 * time.Second // a model synthesis round trip
	writeTimeout  = 30 * time.Second  // a write embeds and indexes before answering
)

func oneLine(text string, width int) string {
	s := strings.Join(strings.Fields(text), " ")
	if r := []rune(s); len(r) > width {
		return string(r[:width])
	}
	return s
}

func datePart(ts string) string {
	if len(ts) >= 10 {
		return ts[:10]
	}
	return ts
}

// emitRaw prints a server response as indented JSON, keeping its key order.
func emitRaw(body []byte) error {
	var out bytes.Buffer
	if err := json.Indent(&out, body, "", "  "); err != nil {
		return err
	}
	render.JSON(out.Bytes())
	return nil
}

func wantsJSON(flag bool) bool { return flag || render.JSONMode() }

func oneOf(value string, choices []string, flag string) error {
	for _, c := range choices {
		if c == value {
			return nil
		}
	}
	return exits.New(fmt.Sprintf("invalid %s %q (choose: %s)", flag, value, strings.Join(choices, ", ")), exits.Usage)
}

// readContent returns the positional words, or stdin for "-" or when stdin is
// a pipe. A bare command at a prompt must show usage, not wait on a read.
func readContent(args []string) (string, error) {
	joined := strings.TrimSpace(strings.Join(args, " "))
	if joined != "" && joined != "-" {
		return joined, nil
	}
	if joined == "-" || !stdinIsTerminal() {
		data, err := io.ReadAll(Stdin)
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(data)), nil
	}
	return "", nil
}

func memoryPath(id string) string { return "/api/v1/memories/" + url.PathEscape(id) }

// fetchMemory GETs one memory and turns its two predictable failures into
// sentences. The server parses the id as a UUID, so a 422 is always a typo.
func fetchMemory(creds *config.Credentials, id string) (map[string]any, []byte, error) {
	resp, err := api.Get(creds, memoryPath(id))
	if err != nil {
		return nil, nil, err
	}
	switch resp.Status {
	case 200:
	case 422:
		return nil, nil, exits.New(fmt.Sprintf("%q is not a memory id. Ids are full UUIDs — copy one from the ID column of `kemory recall`.", id), exits.Usage)
	case 404:
		return nil, nil, exits.New(fmt.Sprintf("No memory %q — it may already be deleted.", id), exits.NotFound)
	default:
		return nil, nil, api.Fail(resp, "GET "+memoryPath(id))
	}
	var m map[string]any
	if err := resp.JSON(&m); err != nil {
		return nil, nil, err
	}
	return m, resp.Body, nil
}

func str(m map[string]any, key string) string {
	switch v := m[key].(type) {
	case string:
		return v
	case nil:
		return ""
	default:
		return fmt.Sprint(v)
	}
}

// --- remember -------------------------------------------------------------------

func newRememberCmd(g *globals) *cobra.Command {
	var (
		namespace, contentType, visibility, team, session, occurredAt string
		tags                                                          []string
		ttl                                                           int
		force, asJSON                                                 bool
	)
	cmd := &cobra.Command{
		Use:   "remember [text...]",
		Short: "Store a memory.",
		Long: "Store a memory. Reads stdin when the text is `-`, or when stdin is a pipe.\n\n" +
			"  kemory remember \"we deploy through ArgoCD only\" --tag deploy\n" +
			"  git log -1 --format=%B | kemory remember - -n project:kemory\n\n" +
			"Without -n, Kemory files the memory by topic.",
		RunE: func(cmd *cobra.Command, args []string) error {
			creds, err := api.RequireCreds(g.env)
			if err != nil {
				return err
			}
			content, err := readContent(args)
			if err != nil {
				return err
			}
			if content == "" {
				return exits.New("Nothing to store. Pass the text, or pipe it in.", exits.Usage)
			}
			body := map[string]any{"content": content, "content_type": contentType}
			if namespace != "" {
				body["namespace"] = namespace
			}
			if len(tags) > 0 {
				body["metadata"] = map[string]any{"tags": tags}
			}
			if cmd.Flags().Changed("ttl") {
				body["ttl_seconds"] = ttl
			}
			if visibility != "" {
				if err := oneOf(visibility, visibilities, "--visibility"); err != nil {
					return err
				}
				body["visibility"] = visibility
			}
			for key, v := range map[string]string{"team_id": team, "session_id": session, "occurred_at": occurredAt} {
				if v != "" {
					body[key] = v
				}
			}
			if force {
				// Only the namespace-similarity check; content dedup still applies.
				body["allow_duplicate"] = true
			}
			resp, err := api.Post(creds, "/api/v1/memories", body, writeTimeout)
			if err != nil {
				return err
			}
			if resp.Status == 409 {
				return namespaceConflict(resp)
			}
			if resp.Status != 200 && resp.Status != 201 {
				return api.Fail(resp, "POST /api/v1/memories")
			}
			if wantsJSON(asJSON) {
				return emitRaw(resp.Body)
			}
			var data struct {
				MemoryID  string `json:"memory_id"`
				Namespace string `json:"namespace"`
				Dedup     struct {
					Deduplicated bool     `json:"deduplicated"`
					Kind         string   `json:"kind"`
					Similarity   *float64 `json:"similarity"`
				} `json:"dedup"`
			}
			if err := resp.JSON(&data); err != nil {
				return err
			}
			// The server answers 201 for a matched duplicate too; saying
			// "stored" would be a lie the user acts on.
			if data.Dedup.Deduplicated {
				detail := orDefault(data.Dedup.Kind, "duplicate")
				if data.Dedup.Similarity != nil {
					detail += fmt.Sprintf(", similarity %.2f", *data.Dedup.Similarity)
				}
				render.Warn(fmt.Sprintf("Already known (%s) — kept the existing memory, nothing new was stored.", detail))
				render.Echo(fmt.Sprintf("  %s  %s", data.MemoryID, data.Namespace))
				return nil
			}
			render.Success("Stored in " + data.Namespace)
			render.Echo("  " + data.MemoryID)
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVarP(&namespace, "namespace", "n", "", "Target namespace. Omit it and Kemory files the memory by topic.")
	f.StringVar(&contentType, "type", "text", "Content type.")
	f.StringArrayVar(&tags, "tag", nil, "Tag the memory. Repeatable.")
	f.IntVar(&ttl, "ttl", 0, "Expire after N seconds (min 60, max 1 year).")
	f.StringVar(&visibility, "visibility", "", "Who can read it: "+strings.Join(visibilities, ", ")+". Default user-private.")
	f.StringVar(&team, "team", "", "Team id. Required with --visibility team.")
	f.StringVar(&session, "session", "", "Session id, to group a run's memories.")
	f.StringVar(&occurredAt, "occurred-at", "", "ISO-8601 time the content happened, when that is not now.")
	f.BoolVar(&force, "force", false, "Accept a namespace that resembles an existing one. Does not bypass content dedup.")
	f.BoolVar(&asJSON, "json", false, "Print the raw response.")
	return cmd
}

// namespaceConflict explains a 409: the namespace resembles one that exists,
// and two namespaces one typo apart split a vault in half.
func namespaceConflict(resp *api.Response) error {
	var payload struct {
		Detail struct {
			Requested string `json:"requested"`
			Suggested []struct {
				Namespace  string  `json:"namespace"`
				Similarity float64 `json:"similarity"`
			} `json:"suggested"`
		} `json:"detail"`
	}
	_ = resp.JSON(&payload)
	msg := fmt.Sprintf("Namespace %q looks like one you already have.", orDefault(payload.Detail.Requested, "?"))
	if len(payload.Detail.Suggested) > 0 {
		var names []string
		for _, s := range payload.Detail.Suggested {
			names = append(names, fmt.Sprintf("%s (%.2f)", s.Namespace, s.Similarity))
		}
		msg += " Existing: " + strings.Join(names, ", ") + "."
	}
	return exits.New(msg+" Re-run with -n <existing namespace>, or --force to create it anyway.", exits.Conflict)
}

// --- recall / ls ----------------------------------------------------------------

type searchOptions struct {
	namespace, contentType string
	tags                   []string
	limit, offset          int
	asJSON                 bool
}

func addSearchFlags(cmd *cobra.Command, o *searchOptions) {
	f := cmd.Flags()
	f.StringVarP(&o.namespace, "namespace", "n", "", "Restrict to one namespace.")
	f.StringVar(&o.contentType, "type", "", "Restrict to a content type.")
	f.StringArrayVar(&o.tags, "tag", nil, "Require this tag. Repeatable.")
	f.IntVar(&o.limit, "limit", 20, "Results per page (1-100).")
	f.IntVar(&o.offset, "offset", 0, "Results to skip.")
	f.BoolVar(&o.asJSON, "json", false, "Print the raw response.")
}

func search(g *globals, o searchOptions, query, mode string, minRelevance *float64) error {
	if o.limit < 1 || o.limit > 100 {
		return exits.New("--limit must be between 1 and 100", exits.Usage)
	}
	if o.offset < 0 {
		return exits.New("--offset must not be negative", exits.Usage)
	}
	creds, err := api.RequireCreds(g.env)
	if err != nil {
		return err
	}
	body := map[string]any{"limit": o.limit, "offset": o.offset, "search_mode": mode}
	if query != "" {
		body["query"] = query
	}
	if o.namespace != "" {
		body["namespace"] = o.namespace
	}
	if o.contentType != "" {
		body["content_type"] = o.contentType
	}
	if len(o.tags) > 0 {
		body["tags"] = o.tags
	}
	if minRelevance != nil {
		body["min_relevance"] = *minRelevance
	}
	resp, err := api.Post(creds, "/api/v1/memories/search", body, searchTimeout)
	if err != nil {
		return err
	}
	if resp.Status == 503 {
		// An encrypted vault matches only through the semantic path; the
		// server says so instead of returning an empty page that reads as
		// "you have nothing like that".
		retry := orDefault(resp.Header.Get("Retry-After"), "a few ")
		return exits.New(fmt.Sprintf("Search is temporarily unavailable for an encrypted vault (a dependency is down). "+
			"Retry in %ss — this is not an empty result.", strings.TrimSpace(retry)), exits.Unavailable)
	}
	if resp.Status != 200 {
		return api.Fail(resp, "POST /api/v1/memories/search")
	}
	if wantsJSON(o.asJSON) {
		return emitRaw(resp.Body)
	}
	var data struct {
		Items                 []map[string]any `json:"items"`
		Total                 *int             `json:"total"`
		TotalMode             string           `json:"total_mode"`
		NoResultsReason       string           `json:"no_results_reason"`
		NoResultsGuidance     string           `json:"no_results_guidance"`
		NoResultsInconclusive bool             `json:"no_results_inconclusive"`
	}
	if err := resp.JSON(&data); err != nil {
		return err
	}
	if len(data.Items) == 0 {
		msg := "Nothing matched."
		if data.NoResultsReason != "" {
			msg += " " + data.NoResultsReason
		}
		render.Warn(msg)
		if data.NoResultsGuidance != "" {
			render.Warn("  " + data.NoResultsGuidance)
		}
		if data.NoResultsInconclusive {
			// "Inconclusive" is not "empty": the vault may hold the answer.
			render.Warn("  This was inconclusive rather than empty — try `kemory recall --mode fts` with an exact phrase, or widen --min-relevance.")
		}
		return nil
	}
	rows := make([][]string, 0, len(data.Items))
	for _, m := range data.Items {
		rows = append(rows, []string{str(m, "memory_id"), str(m, "namespace"), datePart(firstNonEmpty(str(m, "occurred_at"), str(m, "created_at"))), oneLine(str(m, "content"), 140)})
	}
	render.Table("", []string{"ID", "NAMESPACE", "WHEN", "CONTENT"}, rows)
	shown := len(data.Items)
	if data.TotalMode == "page" {
		// Hybrid search is top-K: total is this page, not the corpus.
		render.Muted(fmt.Sprintf("%d shown (top-K; there is no corpus count in hybrid mode)", shown))
		return nil
	}
	total := shown
	if data.Total != nil {
		total = *data.Total
	}
	line := fmt.Sprintf("%d of %d", shown, total)
	if o.namespace == "" {
		line += " (excludes synthetic loadtest:/bench: namespaces; `kemory export` includes them)"
	}
	render.Muted(line)
	return nil
}

func newRecallCmd(g *globals) *cobra.Command {
	var o searchOptions
	var mode string
	var minRelevance float64
	cmd := &cobra.Command{
		Use:   "recall <query...>",
		Short: "Search your memories.",
		Long: "Search your memories. `recall` finds; `kemory ask` explains.\n\n" +
			"  kemory recall \"residency cell\" -n project:kemory --limit 5",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := oneOf(mode, searchModes, "--mode"); err != nil {
				return err
			}
			var min *float64
			if cmd.Flags().Changed("min-relevance") {
				if minRelevance < 0 || minRelevance > 1 {
					return exits.New("--min-relevance must be between 0 and 1", exits.Usage)
				}
				min = &minRelevance
			}
			return search(g, o, strings.Join(args, " "), mode, min)
		},
	}
	addSearchFlags(cmd, &o)
	cmd.Flags().StringVar(&mode, "mode", "hybrid", "hybrid = vector + full text; dense = vector only; fts = exact phrases.")
	cmd.Flags().Float64Var(&minRelevance, "min-relevance", 0, "Floor on raw semantic similarity (0-1).")
	return cmd
}

func newLsCmd(g *globals) *cobra.Command {
	var o searchOptions
	cmd := &cobra.Command{
		Use:   "ls",
		Short: "List memories, newest first.",
		Long:  "List memories, newest first, with no search query.\n\n  kemory ls -n project:kemory --limit 50",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			// Hybrid, not fts: the server rejects an empty fts query to stop
			// unbounded full-table scans.
			return search(g, o, "", "hybrid", nil)
		},
	}
	addSearchFlags(cmd, &o)
	return cmd
}

// --- show / forget ----------------------------------------------------------------

func newShowCmd(g *globals) *cobra.Command {
	var history, similar, asJSON bool
	cmd := &cobra.Command{
		Use:   "show <memory-id>",
		Short: "Print one memory in full.",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			creds, err := api.RequireCreds(g.env)
			if err != nil {
				return err
			}
			m, raw, err := fetchMemory(creds, args[0])
			if err != nil {
				return err
			}
			if wantsJSON(asJSON) {
				return emitRaw(raw)
			}
			tags := "-"
			if meta, ok := m["metadata"].(map[string]any); ok {
				if list, ok := meta["tags"].([]any); ok && len(list) > 0 {
					parts := make([]string, 0, len(list))
					for _, t := range list {
						parts = append(parts, fmt.Sprint(t))
					}
					tags = strings.Join(parts, ", ")
				}
			}
			render.KVs([]render.KV{
				{Key: "id", Value: str(m, "memory_id")},
				{Key: "namespace", Value: str(m, "namespace")},
				{Key: "type", Value: str(m, "content_type")},
				{Key: "tags", Value: tags},
				{Key: "created", Value: str(m, "created_at")},
				{Key: "occurred", Value: orDefault(str(m, "occurred_at"), "-")},
				{Key: "version", Value: str(m, "version")},
				{Key: "tier", Value: str(m, "compression_tier")},
				{Key: "source", Value: str(m, "source_type")},
			})
			render.Echo("")
			render.Echo(str(m, "content"))
			if history {
				// Provenance events (created / updated / consolidated), not
				// revisions: they carry no content.
				subList(creds, args[0], "history", []string{"WHEN", "EVENT", "ACTOR", "REASON"}, func(i map[string]any) []string {
					when := strings.Replace(str(i, "created_at"), "T", " ", 1)
					if len(when) > 19 {
						when = when[:19]
					}
					return []string{when, orDefault(str(i, "event_type"), "-"), orDefault(firstNonEmpty(str(i, "actor_id"), str(i, "actor_type")), "-"), orDefault(oneLine(str(i, "reason"), 80), "-")}
				})
			}
			if similar {
				subList(creds, args[0], "similar", []string{"ID", "NAMESPACE", "CONTENT"}, func(i map[string]any) []string {
					return []string{str(i, "memory_id"), str(i, "namespace"), oneLine(str(i, "content"), 120)}
				})
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&history, "history", false, "Also print the memory's provenance events.")
	cmd.Flags().BoolVar(&similar, "similar", false, "Also print near-duplicates.")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Print the raw response.")
	return cmd
}

func subList(creds *config.Credentials, id, suffix string, columns []string, row func(map[string]any) []string) {
	resp, err := api.Get(creds, memoryPath(id)+"/"+suffix)
	if err != nil || resp.Status != 200 {
		status := "unreachable"
		if resp != nil {
			status = fmt.Sprint(resp.Status)
		}
		render.Warn(fmt.Sprintf("could not load %s: %s", suffix, status))
		return
	}
	var items []map[string]any
	if json.Unmarshal(resp.Body, &items) != nil {
		var wrapped struct {
			Items []map[string]any `json:"items"`
		}
		_ = json.Unmarshal(resp.Body, &wrapped)
		items = wrapped.Items
	}
	render.Echo("")
	if len(items) == 0 {
		render.Muted(fmt.Sprintf("(%s: none)", suffix))
		return
	}
	rows := make([][]string, 0, len(items))
	for _, i := range items {
		rows = append(rows, row(i))
	}
	render.Table(strings.ToUpper(suffix[:1])+suffix[1:], columns, rows)
}

func newForgetCmd(g *globals) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "forget <memory-id>",
		Short: "Delete one memory.",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			creds, err := api.RequireCreds(g.env)
			if err != nil {
				return err
			}
			// Fetch first so the prompt names what is about to go: confirming
			// an opaque id is not consent to delete anything in particular.
			m, _, err := fetchMemory(creds, args[0])
			if err != nil {
				return err
			}
			if !yes {
				render.Echo(fmt.Sprintf("%s  %s", str(m, "namespace"), oneLine(str(m, "content"), 160)))
				if !confirm("Delete this memory?") {
					render.Muted("Aborted.")
					return nil
				}
			}
			resp, err := api.Delete(creds, memoryPath(args[0]))
			if err != nil {
				return err
			}
			if resp.Status != 200 && resp.Status != 204 {
				return api.Fail(resp, "DELETE "+memoryPath(args[0]))
			}
			render.Success("Deleted " + args[0])
			return nil
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip the confirmation prompt.")
	return cmd
}

// confirm asks a yes/no question on stderr and reads the answer from Stdin.
// Anything but y/yes is "no".
func confirm(question string) bool {
	fmt.Fprintf(render.Err, "%s [y/N]: ", question)
	line, _ := bufio.NewReader(Stdin).ReadString('\n')
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes"
}

// --- namespaces ------------------------------------------------------------------

func newNamespacesCmd(g *globals) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "namespaces",
		Short: "List your namespaces and how much is in each.",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			creds, err := api.RequireCreds(g.env)
			if err != nil {
				return err
			}
			resp, err := api.Get(creds, "/api/v1/namespaces")
			if err != nil {
				return err
			}
			if resp.Status != 200 {
				return api.Fail(resp, "GET /api/v1/namespaces")
			}
			if wantsJSON(asJSON) {
				return emitRaw(resp.Body)
			}
			var rows []struct {
				Namespace   string `json:"namespace"`
				Count       int    `json:"count"`
				ChatCount   int    `json:"chat_count"`
				Description string `json:"description"`
			}
			if err := resp.JSON(&rows); err != nil {
				return err
			}
			if len(rows) == 0 {
				render.Muted("(no namespaces yet — `kemory remember` creates one)")
				return nil
			}
			sort.SliceStable(rows, func(i, j int) bool { return rows[i].Count > rows[j].Count })
			table := make([][]string, 0, len(rows))
			for _, r := range rows {
				table = append(table, []string{r.Namespace, fmt.Sprint(r.Count), fmt.Sprint(r.ChatCount), orDefault(oneLine(r.Description, 80), "-")})
			}
			render.Table("", []string{"NAMESPACE", "MEMORIES", "CHATS", "DESCRIPTION"}, table)
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Print the raw response.")

	var summaryJSON bool
	summary := &cobra.Command{
		Use:   "summary <namespace>",
		Short: "Print the consolidated summary of one namespace.",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			creds, err := api.RequireCreds(g.env)
			if err != nil {
				return err
			}
			path := "/api/v1/namespaces/" + url.PathEscape(args[0]) + "/summary"
			resp, err := api.Request(creds, "GET", path, nil, writeTimeout)
			if err != nil {
				return err
			}
			if resp.Status != 200 {
				return api.Fail(resp, "GET "+path)
			}
			if wantsJSON(summaryJSON) {
				return emitRaw(resp.Body)
			}
			var data struct {
				Summary             string `json:"summary"`
				ConsolidatedSummary string `json:"consolidated_summary"`
			}
			if err := resp.JSON(&data); err != nil {
				return err
			}
			text := firstNonEmpty(data.Summary, data.ConsolidatedSummary)
			if text == "" {
				render.Muted(fmt.Sprintf("(%s has no consolidated summary yet)", args[0]))
				return nil
			}
			render.Echo(text)
			return nil
		},
	}
	summary.Flags().BoolVar(&summaryJSON, "json", false, "Print the raw response.")
	cmd.AddCommand(summary)
	return cmd
}

// --- export ----------------------------------------------------------------------

func newExportCmd(g *globals) *cobra.Command {
	var out string
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Write every active memory to a JSON array.",
		Long: "Write every active memory to a JSON array, on stdout or with -o to a file.\n" +
			"Every namespace is included, synthetic loadtest:/bench: ones too, so the\n" +
			"count can exceed the total `kemory ls` reports.",
		Args: cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			creds, err := api.RequireCreds(g.env)
			if err != nil {
				return err
			}
			const path = "/api/v1/memories/export"
			if out == "" {
				_, err := api.StreamTo(creds, path, render.Out, 5*time.Minute)
				return err
			}
			f, err := os.Create(out)
			if err != nil {
				return exits.New(fmt.Sprintf("cannot write %s: %v", out, err), exits.Failure)
			}
			n, err := api.StreamTo(creds, path, f, 5*time.Minute)
			if closeErr := f.Close(); err == nil && closeErr != nil {
				err = closeErr
			}
			if err != nil {
				return err
			}
			render.Success(fmt.Sprintf("Wrote %d bytes to %s", n, out))
			render.Muted("  All namespaces included (even synthetic loadtest:/bench:) — may exceed the `ls` total.")
			return nil
		},
	}
	cmd.Flags().StringVarP(&out, "out", "o", "", "Write to this file instead of stdout.")
	return cmd
}

// --- ask -------------------------------------------------------------------------

func newAskCmd(g *globals) *cobra.Command {
	var limit int
	var types []string
	var noSynth, asJSON bool
	cmd := &cobra.Command{
		Use:   "ask <question...>",
		Short: "Ask your vault a question and get an answer with its evidence.",
		Long: "Ask a question of your vault. Search shows relevant information; ask explains\n" +
			"what it means. Reads memories, captured chats and text files.\n\n" +
			"  kemory ask \"what did we decide about the search architecture\"",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			for _, t := range types {
				if err := oneOf(t, []string{"memory", "chat", "file"}, "--type"); err != nil {
					return err
				}
			}
			creds, err := api.RequireCreds(g.env)
			if err != nil {
				return err
			}
			body := map[string]any{"query": strings.Join(args, " ")}
			if cmd.Flags().Changed("limit") {
				body["limit"] = limit
			}
			if len(types) > 0 {
				body["types"] = types
			}
			if noSynth {
				body["synthesize"] = false
			}
			resp, err := api.Post(creds, "/api/v1/ask", body, askTimeout)
			if err != nil {
				return err
			}
			if resp.Status != 200 {
				return api.Fail(resp, "POST /api/v1/ask")
			}
			if wantsJSON(asJSON) {
				return emitRaw(resp.Body)
			}
			var data struct {
				Synthesized bool             `json:"synthesized"`
				Answer      string           `json:"answer"`
				Items       []map[string]any `json:"items"`
			}
			if err := resp.JSON(&data); err != nil {
				return err
			}
			switch {
			case data.Synthesized && data.Answer != "":
				render.Echo(data.Answer)
			case len(data.Items) > 0:
				render.Warn("No synthesized answer — showing the evidence instead.")
			default:
				render.Warn("No synthesized answer, and nothing matched. Try different phrasing.")
			}
			if len(data.Items) > 0 {
				render.Echo("")
				rows := make([][]string, 0, len(data.Items))
				for _, i := range data.Items {
					// The title identifies a chat or file; its namespace often does not.
					rows = append(rows, []string{orDefault(str(i, "type"), "?"), orDefault(firstNonEmpty(str(i, "title"), str(i, "namespace")), "-"), datePart(str(i, "captured_at")), oneLine(str(i, "snippet"), 160)})
				}
				render.Table(fmt.Sprintf("Evidence (%d)", len(data.Items)), []string{"TYPE", "SOURCE", "WHEN", "SNIPPET"}, rows)
			}
			return nil
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 0, "Most items to ground the answer in.")
	cmd.Flags().StringArrayVar(&types, "type", nil, "Restrict to memory, chat or file. Repeatable.")
	cmd.Flags().BoolVar(&noSynth, "no-synth", false, "Skip the model call; return the evidence only.")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Print the raw response.")
	return cmd
}
