// Package filter evaluates whitelist/blacklist keyword rules against a recipe.
//
// Semantics:
//   - Blacklist wins: any matching blacklist rule rejects the recipe.
//   - Whitelist: if any whitelist rule exists (in the applicable set), the recipe must
//     match at least one; if none exist, the whitelist stage is a no-op.
//   - The cheap feed-metadata pre-filter runs blacklist rules ONLY (absence of a keyword
//     in feed metadata does not mean it's absent from the full recipe), so every whitelist
//     decision is deferred to the post-parse Evaluate call.
//
// Matching is case-insensitive. Note (surfaced in the UI): rules match author-applied
// labels/text, not a nutritional analysis — absence claims like "gluten-free" only match
// when the source actually says so.
package filter

import (
	"regexp"
	"strings"

	"recipearr/internal/store"
)

// Subject is the recipe view a rule is tested against. For the pre-filter only Title and
// Tags are populated; the full Evaluate gets all four from the parsed recipe.
type Subject struct {
	Title       string
	Description string
	Tags        []string
	Ingredients []string
}

// Result is a keep/reject decision with a human-readable reason when rejected.
type Result struct {
	Keep   bool
	Reason string
}

// EvaluateBlacklist runs only the blacklist rules (the cheap pre-filter stage).
func EvaluateBlacklist(rules []*store.FilterRule, s Subject) Result {
	for _, r := range rules {
		if !r.Enabled || r.Mode != store.ModeBlacklist {
			continue
		}
		if ruleMatches(r, s) {
			return Result{Keep: false, Reason: "blacklist: " + r.Keyword}
		}
	}
	return Result{Keep: true}
}

// Evaluate makes the full decision over the parsed recipe.
func Evaluate(rules []*store.FilterRule, s Subject) Result {
	hasWhitelist := false
	whitelistMatched := false
	for _, r := range rules {
		if !r.Enabled {
			continue
		}
		switch r.Mode {
		case store.ModeBlacklist:
			if ruleMatches(r, s) {
				return Result{Keep: false, Reason: "blacklist: " + r.Keyword}
			}
		case store.ModeWhitelist:
			hasWhitelist = true
			if ruleMatches(r, s) {
				whitelistMatched = true
			}
		}
	}
	if hasWhitelist && !whitelistMatched {
		return Result{Keep: false, Reason: "no whitelist keyword matched"}
	}
	return Result{Keep: true}
}

func ruleMatches(r *store.FilterRule, s Subject) bool {
	m := buildMatcher(r)
	switch r.Field {
	case store.FieldTitle:
		return m(s.Title)
	case store.FieldDescription:
		return m(s.Description)
	case store.FieldTags:
		return anyMatch(m, s.Tags)
	case store.FieldIngredients:
		return anyMatch(m, s.Ingredients)
	default: // FieldAny
		return m(s.Title) || m(s.Description) || anyMatch(m, s.Tags) || anyMatch(m, s.Ingredients)
	}
}

// buildMatcher returns a predicate for a rule's keyword + match type. Case-insensitive.
func buildMatcher(r *store.FilterRule) func(string) bool {
	kw := strings.TrimSpace(r.Keyword)
	if kw == "" {
		return func(string) bool { return false }
	}
	switch r.MatchType {
	case store.MatchRegex:
		re, err := regexp.Compile("(?i)" + kw)
		if err != nil {
			return func(string) bool { return false }
		}
		return re.MatchString
	case store.MatchWord:
		re, err := regexp.Compile(`(?i)\b` + regexp.QuoteMeta(kw) + `\b`)
		if err != nil {
			return func(string) bool { return false }
		}
		return re.MatchString
	default: // MatchContains
		lk := strings.ToLower(kw)
		return func(s string) bool { return strings.Contains(strings.ToLower(s), lk) }
	}
}

func anyMatch(m func(string) bool, items []string) bool {
	for _, it := range items {
		if m(it) {
			return true
		}
	}
	return false
}

// ValidateRule checks a rule is well-formed (used at save time by the API).
// Returns "" if valid, otherwise a description of the problem.
func ValidateRule(r *store.FilterRule) string {
	if strings.TrimSpace(r.Keyword) == "" {
		return "keyword is required"
	}
	if r.Mode != store.ModeWhitelist && r.Mode != store.ModeBlacklist {
		return "mode must be whitelist or blacklist"
	}
	switch r.Field {
	case store.FieldAny, store.FieldTitle, store.FieldDescription, store.FieldTags, store.FieldIngredients:
	default:
		return "invalid field"
	}
	switch r.MatchType {
	case store.MatchContains, store.MatchWord:
	case store.MatchRegex:
		if _, err := regexp.Compile(r.Keyword); err != nil {
			return "invalid regex: " + err.Error()
		}
	default:
		return "invalid match type"
	}
	return ""
}
