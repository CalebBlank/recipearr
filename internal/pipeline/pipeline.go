// Package pipeline orchestrates the full import flow:
//
//	poll feed -> dedupe -> blacklist pre-filter -> acquire HTML -> recipe-from-source
//	-> full filter -> enrich/transform -> create -> attach image -> record outcome.
//
// Parsing strategy (validated against the live API): fetch the page ourselves with a browser
// UA and hand the HTML to Tandoor as `data` (dodges its server-side anti-bot + URL-import rate
// limit). If our fetch fails, fall back to letting Tandoor fetch the URL. Filtering and the
// create payload both derive from the recipe-from-source output, so they're always consistent.
package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"recipearr/internal/enrich"
	"recipearr/internal/feed"
	"recipearr/internal/filter"
	"recipearr/internal/store"
	"recipearr/internal/tandoor"
)

type Service struct {
	st        *store.Store
	locks     sync.Map // sourceID -> *sync.Mutex, serializes runs of the same source
	bookMu    sync.Mutex
	bookCache map[string]int // lowercased book name -> id
}

func New(st *store.Store) *Service { return &Service{st: st, bookCache: map[string]int{}} }

// sourceLock returns the mutex serializing processing for a source, so an overlapping
// scheduled poll and a manual "run now" can't both import the same entry.
func (s *Service) sourceLock(id int64) *sync.Mutex {
	m, _ := s.locks.LoadOrStore(id, &sync.Mutex{})
	return m.(*sync.Mutex)
}

// Client builds a Tandoor client from current settings.
func (s *Service) Client() (*tandoor.Client, error) {
	url, _ := s.st.GetSetting("tandoor_url", "")
	token, _ := s.st.GetSetting("tandoor_token", "")
	if url == "" || token == "" {
		return nil, errors.New("Tandoor URL/token not configured")
	}
	return tandoor.New(url, token), nil
}

// Result tallies the outcome of processing a source.
type Result struct {
	Imported    int `json:"imported"`
	FilteredOut int `json:"filtered_out"`
	Failed      int `json:"failed"`
	Duplicate   int `json:"duplicate"`
	Skipped     int `json:"skipped"`
}

// ProcessOpts controls a source run.
type ProcessOpts struct {
	Import bool // false = seed only (record entries as seen without importing)
	Limit  int  // max NEW entries to act on (0 = all unseen)
}

// ProcessSource polls a source's feed and runs each new entry through the pipeline.
func (s *Service) ProcessSource(ctx context.Context, src *store.Source, po ProcessOpts) (Result, error) {
	var res Result

	lk := s.sourceLock(src.ID)
	lk.Lock()
	defer lk.Unlock()

	tc, err := s.Client()
	if err != nil {
		_ = s.st.UpdateSourceStatus(src.ID, "error", err.Error())
		return res, err
	}
	entries, err := feed.Fetch(ctx, src.FeedURL)
	if err != nil {
		_ = s.st.UpdateSourceStatus(src.ID, "error", "feed: "+err.Error())
		return res, err
	}

	rules, _ := s.st.RulesForSource(src.ID)
	opts := s.optionsForSource(src)
	sid := src.ID

	acted := 0
	for _, e := range entries {
		if po.Limit > 0 && acted >= po.Limit {
			break
		}
		exists, _ := s.st.ItemExists(e.GUID, e.URL)
		if exists {
			continue
		}
		acted++

		if !po.Import {
			_ = s.st.CreateItem(&store.Item{
				SourceID: &sid, GUID: e.GUID, URL: e.URL, Title: e.Title,
				Status: store.StatusSkipped, FilterReason: "seeded on add (watch new only)",
				ProcessedAt: nowPtr(),
			})
			res.Skipped++
			continue
		}

		it := s.importEntry(ctx, tc, &sid, e, rules, opts, src.Name)
		tally(&res, it.Status)

		if err := ctx.Err(); err != nil {
			break
		}
	}

	_ = s.st.UpdateSourceStatus(src.ID, "ok", "")
	return res, nil
}

// Poll runs a source correctly based on whether it has been seeded yet. This is what the
// scheduler and "run now" call. An unseeded source gets its initial reconciliation (backlog
// import, or a watch-only seed) and is marked seeded ONLY on success; a seeded source gets a
// normal "import new entries" poll. Because seeding is persisted, a transient feed error just
// means "still not seeded, retry next tick" — it can never dump the whole feed window into
// Tandoor, and watch-only intent survives restarts.
func (s *Service) Poll(ctx context.Context, src *store.Source) (Result, error) {
	if !src.Seeded {
		po := ProcessOpts{Import: false} // watch-only seed
		if src.BacklogOnAdd {
			po = ProcessOpts{Import: true, Limit: src.BacklogLimit}
		}
		res, err := s.ProcessSource(ctx, src, po)
		if err == nil {
			_ = s.st.MarkSeeded(src.ID)
		}
		return res, err
	}
	return s.ProcessSource(ctx, src, ProcessOpts{Import: true})
}

// ImportURL imports a single recipe URL on demand (ad-hoc box and bulk paste/OPML).
// sourceID may be nil (uses global rules) or point at a source (uses its rules + defaults).
func (s *Service) ImportURL(ctx context.Context, rawURL string, sourceID *int64) (*store.Item, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return nil, errors.New("empty url")
	}
	tc, err := s.Client()
	if err != nil {
		return nil, err
	}

	var rules []*store.FilterRule
	var src *store.Source
	if sourceID != nil {
		rules, _ = s.st.RulesForSource(*sourceID)
		src, _ = s.st.GetSource(*sourceID)
	} else {
		rules, _ = s.st.GlobalRules()
	}
	opts := s.optionsForSource(src)

	bookName := ""
	if src != nil {
		bookName = src.Name
	} else {
		bookName = hostLabel(rawURL)
	}

	it := &store.Item{SourceID: sourceID, URL: rawURL, Status: store.StatusDiscovered}
	if err := s.st.CreateItem(it); err != nil {
		return nil, err
	}
	// Ad-hoc imports have no feed metadata, so there's no cheap pre-filter stage.
	s.importCore(ctx, tc, it, rules, opts, bookName)
	return it, nil
}

// importEntry handles a feed entry: cheap blacklist pre-filter, then the shared core.
func (s *Service) importEntry(ctx context.Context, tc *tandoor.Client, sid *int64, e feed.Entry, rules []*store.FilterRule, opts enrich.Options, bookName string) *store.Item {
	it := &store.Item{SourceID: sid, GUID: e.GUID, URL: e.URL, Title: e.Title, Status: store.StatusDiscovered}
	if err := s.st.CreateItem(it); err != nil {
		// Lost the race on the unique (source_id, guid) index (or a DB error): another run
		// owns this entry. Skip without importing so we never create a duplicate recipe.
		return it
	}

	pre := filter.EvaluateBlacklist(rules, filter.Subject{Title: e.Title, Tags: e.Categories})
	if !pre.Keep {
		s.finalize(it, store.StatusFilteredOut, pre.Reason, "")
		return it
	}
	s.importCore(ctx, tc, it, rules, opts, bookName)
	return it
}

// importCore is the shared parse -> filter -> enrich -> create -> image -> book flow.
func (s *Service) importCore(ctx context.Context, tc *tandoor.Client, it *store.Item, rules []*store.FilterRule, opts enrich.Options, bookName string) {
	rfs, msg := parseRecipe(ctx, tc, it.URL)
	if rfs == nil || rfs.Recipe == nil {
		s.finalize(it, store.StatusFailed, "", orDefault(msg, "could not parse recipe"))
		return
	}
	rec := rfs.Recipe
	it.RawTags = tagsCSV(rec.Keywords)

	if r := filter.Evaluate(rules, subjectFromRecipe(rec)); !r.Keep {
		if rec.Name != "" {
			it.Title = rec.Name
		}
		s.finalize(it, store.StatusFilteredOut, r.Reason, "")
		return
	}
	if len(rfs.Duplicates) > 0 {
		it.Title = rec.Name
		s.finalize(it, store.StatusDuplicate, "already in Tandoor: "+rfs.Duplicates[0].Name, "")
		return
	}

	payload := enrich.Transform(rec, opts)
	id, err := tc.CreateRecipe(ctx, payload)
	if err != nil {
		it.Title = rec.Name
		s.finalize(it, store.StatusFailed, "", "create: "+err.Error())
		return
	}
	id64 := int64(id)
	it.TandoorRecipeID = &id64
	it.Title = rec.Name

	if rec.ImageURL != "" {
		// Best effort: a missing image shouldn't fail an otherwise-good import.
		_ = tc.AttachImageURL(ctx, id, rec.ImageURL)
	}
	// Best effort: organize into a recipe book named after the source/site.
	s.maybeAddToBook(ctx, tc, id, bookName)
	s.finalize(it, store.StatusImported, "", "")
}

// maybeAddToBook adds a recipe to a book named bookName, when the feature is enabled.
func (s *Service) maybeAddToBook(ctx context.Context, tc *tandoor.Client, recipeID int, bookName string) {
	if !s.boolSetting("organize_into_books", false) {
		return
	}
	bookName = strings.TrimSpace(bookName)
	if bookName == "" {
		return
	}
	bid, err := s.ensureBook(ctx, tc, bookName)
	if err != nil {
		return
	}
	_ = tc.AddRecipeToBook(ctx, bid, recipeID)
}

// ensureBook returns the id of the book named name, creating it if needed. Results are cached
// per process; the whole operation is serialized so concurrent imports don't create duplicates.
func (s *Service) ensureBook(ctx context.Context, tc *tandoor.Client, name string) (int, error) {
	key := strings.ToLower(strings.TrimSpace(name))
	s.bookMu.Lock()
	defer s.bookMu.Unlock()
	if id, ok := s.bookCache[key]; ok {
		return id, nil
	}
	books, err := tc.ListBooks(ctx)
	if err != nil {
		return 0, err
	}
	for _, b := range books {
		s.bookCache[strings.ToLower(strings.TrimSpace(b.Name))] = b.ID
	}
	if id, ok := s.bookCache[key]; ok {
		return id, nil
	}
	id, err := tc.CreateBook(ctx, strings.TrimSpace(name))
	if err != nil {
		return 0, err
	}
	s.bookCache[key] = id
	return id, nil
}

// parseRecipe returns a parsed recipe, preferring our own fetch (HTML as `data`) and falling
// back to letting Tandoor fetch the URL. Returns (nil, msg) on failure.
func parseRecipe(ctx context.Context, tc *tandoor.Client, url string) (*tandoor.RFSResponse, string) {
	pageHTML := ""
	if h, err := feed.FetchHTML(ctx, url); err == nil && h != "" {
		pageHTML = h
		if rfs, err := tc.RecipeFromSource(ctx, url, pageHTML); err == nil && !rfs.Error && rfs.Recipe != nil {
			fillDescriptionFallback(rfs.Recipe, pageHTML)
			return rfs, ""
		}
	}
	rfs, err := tc.RecipeFromSource(ctx, url, "")
	if err != nil {
		return nil, err.Error()
	}
	if rfs.Error || rfs.Recipe == nil {
		return nil, rfs.Msg
	}
	fillDescriptionFallback(rfs.Recipe, pageHTML) // no-op if no HTML or description already present
	return rfs, ""
}

// fillDescriptionFallback fills an empty recipe description from the page's meta/og:description.
// Minimal recipe cards (e.g. Smitten Kitchen) often ship no recipe description even though the
// page has a perfectly good meta description.
func fillDescriptionFallback(rec *tandoor.RFSRecipe, pageHTML string) {
	if rec == nil || pageHTML == "" || strings.TrimSpace(rec.Description) != "" {
		return
	}
	if d := metaDescription(pageHTML); d != "" {
		rec.Description = d
	}
}

var (
	metaDescRe1 = regexp.MustCompile(`(?is)<meta[^>]*\b(?:property|name)\s*=\s*["'](?:og:description|description)["'][^>]*\bcontent\s*=\s*["']([^"']*)["']`)
	metaDescRe2 = regexp.MustCompile(`(?is)<meta[^>]*\bcontent\s*=\s*["']([^"']*)["'][^>]*\b(?:property|name)\s*=\s*["'](?:og:description|description)["']`)
)

// metaDescription extracts the page's og:description or <meta name="description"> (either
// attribute order). Returns raw text; the enrich step later unescapes/cleans it.
func metaDescription(pageHTML string) string {
	if m := metaDescRe1.FindStringSubmatch(pageHTML); m != nil {
		return strings.TrimSpace(m[1])
	}
	if m := metaDescRe2.FindStringSubmatch(pageHTML); m != nil {
		return strings.TrimSpace(m[1])
	}
	return ""
}

func (s *Service) finalize(it *store.Item, status, reason, errMsg string) {
	it.Status = status
	it.FilterReason = reason
	it.Error = errMsg
	it.ProcessedAt = nowPtr()
	_ = s.st.UpdateItem(it)
}

// optionsForSource builds enrichment options from settings, layering source default keywords.
func (s *Service) optionsForSource(src *store.Source) enrich.Options {
	o := enrich.DefaultOptions()
	o.Communize = s.boolSetting("enrich_communize", true)
	o.CleanDescription = s.boolSetting("enrich_clean_description", true)
	o.CurateTags = s.boolSetting("enrich_curate_tags", true)
	o.AllocateSteps = s.boolSetting("enrich_allocate_steps", true)
	o.DescriptionMode, _ = s.st.GetSetting("enrich_description_mode", "clean")

	if raw, _ := s.st.GetSetting("enrich_aliases", ""); raw != "" {
		var m map[string]string
		if json.Unmarshal([]byte(raw), &m) == nil {
			low := make(map[string]string, len(m))
			for k, v := range m {
				low[strings.ToLower(strings.TrimSpace(k))] = v
			}
			o.Aliases = low
		}
	}
	if raw, _ := s.st.GetSetting("enrich_tag_denylist", ""); raw != "" {
		var l []string
		if json.Unmarshal([]byte(raw), &l) == nil && len(l) > 0 {
			for i := range l {
				l[i] = strings.ToLower(l[i])
			}
			o.TagDenylist = l
		}
	}

	var extras []string
	if src != nil {
		for _, k := range strings.Split(src.DefaultKeywords, ",") {
			if t := strings.TrimSpace(k); t != "" {
				extras = append(extras, t)
			}
		}
	}
	o.ExtraKeywords = extras
	return o
}

func (s *Service) boolSetting(key string, def bool) bool {
	v, _ := s.st.GetSetting(key, "")
	if v == "" {
		return def
	}
	return v == "1" || strings.EqualFold(v, "true")
}

// ---- helpers ----

func subjectFromRecipe(r *tandoor.RFSRecipe) filter.Subject {
	var tags, ings []string
	for _, k := range r.Keywords {
		if k.Name != "" {
			tags = append(tags, k.Name)
		}
		if k.Label != "" && k.Label != k.Name {
			tags = append(tags, k.Label)
		}
	}
	for _, st := range r.Steps {
		for _, ing := range st.Ingredients {
			if ing.Food != nil && ing.Food.Name != "" {
				ings = append(ings, ing.Food.Name)
			}
			if ing.OriginalText != "" {
				ings = append(ings, ing.OriginalText)
			}
		}
	}
	return filter.Subject{Title: r.Name, Description: r.Description, Tags: tags, Ingredients: ings}
}

func tagsCSV(ks []tandoor.RFSKeyword) string {
	var names []string
	for _, k := range ks {
		if k.Name != "" {
			names = append(names, k.Name)
		}
	}
	return strings.Join(names, ", ")
}

func tally(res *Result, status string) {
	switch status {
	case store.StatusImported:
		res.Imported++
	case store.StatusFilteredOut:
		res.FilteredOut++
	case store.StatusFailed:
		res.Failed++
	case store.StatusDuplicate:
		res.Duplicate++
	case store.StatusSkipped:
		res.Skipped++
	}
}

func nowPtr() *time.Time { t := time.Now().UTC(); return &t }

func orDefault(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}

// hostLabel derives a book name from a URL's host (e.g. "seriouseats.com"), used for ad-hoc
// imports that have no named source.
func hostLabel(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
}
