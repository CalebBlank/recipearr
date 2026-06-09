package enrich

import (
	"sort"
	"strings"
)

// defaultDietTaxonomy maps a tag to the trigger phrases that imply it. Detection runs over the
// title + description + the site's own keyword labels (NOT ingredients, to avoid false hits).
func defaultDietTaxonomy() map[string][]string {
	return map[string][]string{
		"vegan":       {"vegan"},
		"vegetarian":  {"vegetarian", "meatless"},
		"gluten-free": {"gluten-free", "gluten free"},
		"dairy-free":  {"dairy-free", "dairy free"},
		"nut-free":    {"nut-free", "nut free"},
		"keto":        {"keto", "ketogenic"},
		"paleo":       {"paleo"},
		"low-carb":    {"low-carb", "low carb"},
		"whole30":     {"whole30", "whole 30"},
		"quick":       {"quick", "weeknight", "15 minute", "20 minute", "30 minute", "15-minute", "20-minute", "30-minute"},
		"one-pot":     {"one-pot", "one pot", "one-pan", "one pan", "sheet pan", "sheet-pan"},
	}
}

// defaultTagDenylist drops obviously non-useful tags (kept minimal; import_keyword already
// filters most noise, this is a secondary user-tunable backstop for domains).
func defaultTagDenylist() []string {
	return []string{".com", ".net", ".org", "http"}
}

// detectDietTags returns the tags whose trigger phrases appear in the (already lowercased) text.
func detectDietTags(textLower string, taxonomy map[string][]string) []string {
	var out []string
	for tag, triggers := range taxonomy {
		for _, tr := range triggers {
			if strings.Contains(textLower, tr) {
				out = append(out, tag)
				break
			}
		}
	}
	sort.Strings(out) // deterministic output
	return out
}
