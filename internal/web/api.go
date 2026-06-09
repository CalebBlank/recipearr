package web

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"recipearr/internal/filter"
	"recipearr/internal/pipeline"
	"recipearr/internal/store"
	"recipearr/internal/tandoor"
)

func pathID(r *http.Request) (int64, error) {
	return strconv.ParseInt(r.PathValue("id"), 10, 64)
}

func boolStr(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

// ---- settings ----

type settingsDTO struct {
	TandoorURL             string            `json:"tandoor_url"`
	TandoorToken           string            `json:"tandoor_token"` // write-only; blank on GET
	TokenSet               bool              `json:"token_set"`
	EnrichCommunize        bool              `json:"enrich_communize"`
	EnrichCleanDescription bool              `json:"enrich_clean_description"`
	EnrichCurateTags       bool              `json:"enrich_curate_tags"`
	EnrichAllocateSteps    bool              `json:"enrich_allocate_steps"`
	DescriptionMode        string            `json:"enrich_description_mode"`
	TagDenylist            []string          `json:"enrich_tag_denylist"`
	Aliases                map[string]string `json:"enrich_aliases"`
	OrganizeIntoBooks      bool              `json:"organize_into_books"`
}

func (s *Server) getStr(k, d string) string { v, _ := s.st.GetSetting(k, d); return v }
func (s *Server) getBool(k string, d bool) bool {
	v, _ := s.st.GetSetting(k, boolStr(d))
	return v == "1" || strings.EqualFold(v, "true")
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	dto := settingsDTO{
		TandoorURL:             s.getStr("tandoor_url", ""),
		TokenSet:               s.getStr("tandoor_token", "") != "",
		EnrichCommunize:        s.getBool("enrich_communize", true),
		EnrichCleanDescription: s.getBool("enrich_clean_description", true),
		EnrichCurateTags:       s.getBool("enrich_curate_tags", true),
		EnrichAllocateSteps:    s.getBool("enrich_allocate_steps", true),
		DescriptionMode:        s.getStr("enrich_description_mode", "clean"),
		OrganizeIntoBooks:      s.getBool("organize_into_books", false),
	}
	if raw := s.getStr("enrich_tag_denylist", ""); raw != "" {
		_ = json.Unmarshal([]byte(raw), &dto.TagDenylist)
	}
	if raw := s.getStr("enrich_aliases", ""); raw != "" {
		_ = json.Unmarshal([]byte(raw), &dto.Aliases)
	}
	writeJSON(w, http.StatusOK, dto)
}

func (s *Server) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	var in settingsDTO
	if err := readJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if u := strings.TrimSpace(in.TandoorURL); u != "" { // empty = keep existing URL (don't wipe)
		_ = s.st.SetSetting("tandoor_url", u)
	}
	if t := strings.TrimSpace(in.TandoorToken); t != "" { // empty = keep existing token
		_ = s.st.SetSetting("tandoor_token", t)
	}
	_ = s.st.SetSetting("enrich_communize", boolStr(in.EnrichCommunize))
	_ = s.st.SetSetting("enrich_clean_description", boolStr(in.EnrichCleanDescription))
	_ = s.st.SetSetting("enrich_curate_tags", boolStr(in.EnrichCurateTags))
	_ = s.st.SetSetting("enrich_allocate_steps", boolStr(in.EnrichAllocateSteps))
	if in.DescriptionMode != "" {
		_ = s.st.SetSetting("enrich_description_mode", in.DescriptionMode)
	}
	_ = s.st.SetSetting("organize_into_books", boolStr(in.OrganizeIntoBooks))
	if in.TagDenylist != nil {
		b, _ := json.Marshal(in.TagDenylist)
		_ = s.st.SetSetting("enrich_tag_denylist", string(b))
	}
	if in.Aliases != nil {
		b, _ := json.Marshal(in.Aliases)
		_ = s.st.SetSetting("enrich_aliases", string(b))
	}
	s.handleGetSettings(w, r)
}

func (s *Server) handleTestConnection(w http.ResponseWriter, r *http.Request) {
	var in struct {
		TandoorURL   string `json:"tandoor_url"`
		TandoorToken string `json:"tandoor_token"`
	}
	_ = readJSON(r, &in)
	url := strings.TrimSpace(in.TandoorURL)
	token := strings.TrimSpace(in.TandoorToken)
	if url == "" {
		url = s.getStr("tandoor_url", "")
	}
	if token == "" {
		token = s.getStr("tandoor_token", "")
	}
	if url == "" || token == "" {
		writeErr(w, http.StatusBadRequest, "Tandoor URL and token required")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	count, err := tandoor.New(url, token).TestConnection(ctx)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "recipe_count": count})
}

// ---- sources ----

func (s *Server) handleListSources(w http.ResponseWriter, r *http.Request) {
	srcs, err := s.st.ListSources()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if srcs == nil {
		srcs = []*store.Source{}
	}
	writeJSON(w, http.StatusOK, srcs)
}

func (s *Server) handleCreateSource(w http.ResponseWriter, r *http.Request) {
	var src store.Source
	if err := readJSON(r, &src); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	src.FeedURL = strings.TrimSpace(src.FeedURL)
	if src.FeedURL == "" {
		writeErr(w, http.StatusBadRequest, "feed_url is required")
		return
	}
	if strings.TrimSpace(src.Name) == "" {
		src.Name = src.FeedURL
	}
	if src.PollIntervalMinutes <= 0 {
		src.PollIntervalMinutes = 60
	}
	if src.BacklogLimit <= 0 {
		src.BacklogLimit = 25
	}
	if err := s.st.CreateSource(&src); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Kick off the initial reconciliation in the background. Poll seeds-or-backlogs based on
	// the source's (unseeded) state and persists the result, so even if this goroutine dies
	// the scheduler will complete the seed correctly rather than mass-importing.
	if src.Enabled {
		go func(src store.Source) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
			defer cancel()
			_, _ = s.svc.Poll(ctx, &src)
		}(src)
	}
	writeJSON(w, http.StatusCreated, src)
}

func (s *Server) handleUpdateSource(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	var src store.Source
	if err := readJSON(r, &src); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	src.ID = id
	if src.PollIntervalMinutes <= 0 {
		src.PollIntervalMinutes = 60
	}
	if err := s.st.UpdateSource(&src); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	updated, _ := s.st.GetSource(id)
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) handleDeleteSource(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := s.st.DeleteSource(id); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRunSource(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	src, err := s.st.GetSource(id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "source not found")
		return
	}
	backlog := r.URL.Query().Get("backlog") == "1"
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
		defer cancel()
		if backlog {
			// Explicit "import recent N now": grab the backlog window and mark seeded.
			_, _ = s.svc.ProcessSource(ctx, src, pipeline.ProcessOpts{Import: true, Limit: src.BacklogLimit})
			_ = s.st.MarkSeeded(src.ID)
		} else {
			_, _ = s.svc.Poll(ctx, src)
		}
	}()
	writeJSON(w, http.StatusAccepted, map[string]bool{"started": true})
}

// ---- rules ----

func (s *Server) handleListRules(w http.ResponseWriter, r *http.Request) {
	rules, err := s.st.ListRules()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if rules == nil {
		rules = []*store.FilterRule{}
	}
	writeJSON(w, http.StatusOK, rules)
}

func (s *Server) handleCreateRule(w http.ResponseWriter, r *http.Request) {
	var rule store.FilterRule
	if err := readJSON(r, &rule); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if rule.Field == "" {
		rule.Field = store.FieldAny
	}
	if rule.MatchType == "" {
		rule.MatchType = store.MatchContains
	}
	if msg := filter.ValidateRule(&rule); msg != "" {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	if err := s.st.CreateRule(&rule); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, rule)
}

func (s *Server) handleUpdateRule(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	var rule store.FilterRule
	if err := readJSON(r, &rule); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	rule.ID = id
	if msg := filter.ValidateRule(&rule); msg != "" {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	if err := s.st.UpdateRule(&rule); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, rule)
}

func (s *Server) handleDeleteRule(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := s.st.DeleteRule(id); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- items ----

func (s *Server) handleListItems(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := s.st.ListItems(status, limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if items == nil {
		items = []*store.Item{}
	}
	counts, _ := s.st.StatusCounts()
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "counts": counts})
}

// ---- ad-hoc / bulk import ----

func (s *Server) handleImport(w http.ResponseWriter, r *http.Request) {
	var in struct {
		URL  string   `json:"url"`
		URLs []string `json:"urls"`
	}
	if err := readJSON(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	// Bulk: process in the background, return immediately.
	if len(in.URLs) > 0 {
		urls := in.URLs
		go func() {
			for _, u := range urls {
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
				_, _ = s.svc.ImportURL(ctx, u, nil)
				cancel()
			}
		}()
		writeJSON(w, http.StatusAccepted, map[string]any{"started": true, "count": len(urls)})
		return
	}

	// Single: run synchronously so the user sees the result.
	if strings.TrimSpace(in.URL) == "" {
		writeErr(w, http.StatusBadRequest, "url or urls required")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
	defer cancel()
	it, err := s.svc.ImportURL(ctx, in.URL, nil)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, it)
}
