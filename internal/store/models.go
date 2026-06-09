package store

import "time"

// Item status values.
const (
	StatusDiscovered = "discovered"
	StatusFilteredOut = "filtered_out"
	StatusImported   = "imported"
	StatusFailed     = "failed"
	StatusDuplicate  = "duplicate"
	StatusSkipped    = "skipped" // seen but intentionally not imported (seeded on add)
)

// Filter rule modes and match types.
const (
	ModeWhitelist = "whitelist"
	ModeBlacklist = "blacklist"

	FieldAny         = "any"
	FieldTitle       = "title"
	FieldDescription = "description"
	FieldTags        = "tags"
	FieldIngredients = "ingredients"

	MatchContains = "contains"
	MatchWord     = "word"
	MatchRegex    = "regex"
)

// Source is a watched website (an RSS/Atom feed).
type Source struct {
	ID                  int64      `json:"id"`
	Name                string     `json:"name"`
	FeedURL             string     `json:"feed_url"`
	SiteURL             string     `json:"site_url"`
	Enabled             bool       `json:"enabled"`
	PollIntervalMinutes int        `json:"poll_interval_minutes"`
	BacklogOnAdd        bool       `json:"backlog_on_add"`
	BacklogLimit        int        `json:"backlog_limit"`
	// Seeded is set once a source's initial reconciliation with its feed has happened
	// (backlog import or watch-only seed). Until then, scheduled polls must NOT mass-import.
	Seeded              bool       `json:"seeded"`
	DefaultKeywords     string     `json:"default_keywords"` // comma-separated
	CreatedAt           time.Time  `json:"created_at"`
	LastCheckedAt       *time.Time `json:"last_checked_at"`
	LastStatus          string     `json:"last_status"`
	LastError           string     `json:"last_error"`
}

// FilterRule is one whitelist/blacklist keyword rule, global (SourceID nil) or per-source.
type FilterRule struct {
	ID        int64     `json:"id"`
	SourceID  *int64    `json:"source_id"`
	Mode      string    `json:"mode"`
	Field     string    `json:"field"`
	Keyword   string    `json:"keyword"`
	MatchType string    `json:"match_type"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
}

// Item is one discovered recipe and its journey through the pipeline (the audit trail).
type Item struct {
	ID              int64      `json:"id"`
	SourceID        *int64     `json:"source_id"` // nil for ad-hoc URL imports
	GUID            string     `json:"guid"`
	URL             string     `json:"url"`
	Title           string     `json:"title"`
	Status          string     `json:"status"`
	FilterReason    string     `json:"filter_reason"`
	TandoorRecipeID *int64     `json:"tandoor_recipe_id"`
	RawTags         string     `json:"raw_tags"`
	Error           string     `json:"error"`
	DiscoveredAt    time.Time  `json:"discovered_at"`
	ProcessedAt     *time.Time `json:"processed_at"`
}
