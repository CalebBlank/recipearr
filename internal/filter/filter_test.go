package filter

import (
	"testing"

	"recipearr/internal/store"
)

func rule(mode, field, keyword, match string) *store.FilterRule {
	return &store.FilterRule{Mode: mode, Field: field, Keyword: keyword, MatchType: match, Enabled: true}
}

func TestEvaluate(t *testing.T) {
	veganInDescription := Subject{
		Title:       "15 Minute Lemon Pasta",
		Description: "Celebrate spring with this vegan Lemon Pasta recipe.",
	}

	tests := []struct {
		name      string
		rules     []*store.FilterRule
		subject   Subject
		wantKeep  bool
	}{
		{
			name:     "no rules keeps everything",
			rules:    nil,
			subject:  Subject{Title: "Anything"},
			wantKeep: true,
		},
		{
			name:     "blacklist match rejects",
			rules:    []*store.FilterRule{rule(store.ModeBlacklist, store.FieldAny, "pork", store.MatchContains)},
			subject:  Subject{Title: "Slow Pork Shoulder", Ingredients: []string{"2 lb pork"}},
			wantKeep: false,
		},
		{
			name:     "blacklist no match keeps",
			rules:    []*store.FilterRule{rule(store.ModeBlacklist, store.FieldAny, "pork", store.MatchContains)},
			subject:  Subject{Title: "Tofu Stir Fry"},
			wantKeep: true,
		},
		{
			name:     "whitelist required, not matched rejects",
			rules:    []*store.FilterRule{rule(store.ModeWhitelist, store.FieldAny, "vegan", store.MatchContains)},
			subject:  Subject{Title: "Chicken Soup"},
			wantKeep: false,
		},
		{
			name:     "whitelist matched in description keeps (lemon pasta case)",
			rules:    []*store.FilterRule{rule(store.ModeWhitelist, store.FieldAny, "vegan", store.MatchContains)},
			subject:  veganInDescription,
			wantKeep: true,
		},
		{
			name: "blacklist wins over matched whitelist",
			rules: []*store.FilterRule{
				rule(store.ModeWhitelist, store.FieldAny, "vegan", store.MatchContains),
				rule(store.ModeBlacklist, store.FieldIngredients, "peanuts", store.MatchContains),
			},
			subject:  Subject{Title: "Vegan Satay", Ingredients: []string{"peanuts", "tofu"}},
			wantKeep: false,
		},
		{
			name:     "field targeting: ingredients rule ignores title-only hit",
			rules:    []*store.FilterRule{rule(store.ModeBlacklist, store.FieldIngredients, "egg", store.MatchContains)},
			subject:  Subject{Title: "Egg Salad Sandwich", Ingredients: []string{"mayonnaise", "mustard", "celery"}},
			wantKeep: true, // "egg" is only in the title; the rule is ingredient-scoped
		},
		{
			name:     "word match does not match substring",
			rules:    []*store.FilterRule{rule(store.ModeBlacklist, store.FieldAny, "ham", store.MatchWord)},
			subject:  Subject{Title: "Graham Cracker Crust"},
			wantKeep: true,
		},
		{
			name:     "contains match catches substring",
			rules:    []*store.FilterRule{rule(store.ModeBlacklist, store.FieldAny, "ham", store.MatchContains)},
			subject:  Subject{Title: "Graham Cracker Crust"},
			wantKeep: false,
		},
		{
			name:     "regex match",
			rules:    []*store.FilterRule{rule(store.ModeBlacklist, store.FieldTitle, "gluten[- ]?free", store.MatchRegex)},
			subject:  Subject{Title: "Gluten-Free Brownies"},
			wantKeep: false,
		},
		{
			name: "disabled rule ignored",
			rules: []*store.FilterRule{func() *store.FilterRule {
				r := rule(store.ModeBlacklist, store.FieldAny, "pork", store.MatchContains)
				r.Enabled = false
				return r
			}()},
			subject:  Subject{Title: "Pork Belly"},
			wantKeep: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Evaluate(tc.rules, tc.subject)
			if got.Keep != tc.wantKeep {
				t.Errorf("Evaluate() keep = %v (reason %q), want %v", got.Keep, got.Reason, tc.wantKeep)
			}
		})
	}
}

// The "egg" in ingredients test above contains a subtlety worth pinning down:
// "eggplant" DOES contain "egg" as a substring, so a contains-match on ingredients matches it.
func TestIngredientsContainsSubtlety(t *testing.T) {
	rules := []*store.FilterRule{rule(store.ModeBlacklist, store.FieldIngredients, "egg", store.MatchContains)}
	// eggplant contains "egg" -> rejected with contains
	if Evaluate(rules, Subject{Ingredients: []string{"eggplant"}}).Keep {
		t.Error("contains 'egg' should match 'eggplant'")
	}
	// with word match, "egg" should NOT match "eggplant"
	wordRules := []*store.FilterRule{rule(store.ModeBlacklist, store.FieldIngredients, "egg", store.MatchWord)}
	if !Evaluate(wordRules, Subject{Ingredients: []string{"eggplant"}}).Keep {
		t.Error("word 'egg' should not match 'eggplant'")
	}
	// word match SHOULD match a standalone "egg"
	if Evaluate(wordRules, Subject{Ingredients: []string{"1 large egg"}}).Keep {
		t.Error("word 'egg' should match '1 large egg'")
	}
}

func TestEvaluateBlacklistOnly(t *testing.T) {
	rules := []*store.FilterRule{
		rule(store.ModeWhitelist, store.FieldAny, "vegan", store.MatchContains),
		rule(store.ModeBlacklist, store.FieldAny, "pork", store.MatchContains),
	}
	// Pre-filter ignores the whitelist: a non-vegan, non-pork subject must pass.
	if got := EvaluateBlacklist(rules, Subject{Title: "Chicken Soup"}); !got.Keep {
		t.Errorf("pre-filter should ignore whitelist, got reject: %q", got.Reason)
	}
	// But a blacklisted subject is rejected early.
	if got := EvaluateBlacklist(rules, Subject{Title: "Pork Ramen"}); got.Keep {
		t.Error("pre-filter should reject blacklisted subject")
	}
}

func TestValidateRule(t *testing.T) {
	if msg := ValidateRule(rule(store.ModeBlacklist, store.FieldAny, "", store.MatchContains)); msg == "" {
		t.Error("empty keyword should be invalid")
	}
	if msg := ValidateRule(rule(store.ModeBlacklist, store.FieldAny, "x[", store.MatchRegex)); msg == "" {
		t.Error("invalid regex should be reported")
	}
	if msg := ValidateRule(rule(store.ModeWhitelist, store.FieldTags, "vegan", store.MatchContains)); msg != "" {
		t.Errorf("valid rule reported invalid: %s", msg)
	}
}
