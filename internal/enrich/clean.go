package enrich

import (
	"html"
	"regexp"
	"strings"
)

var (
	htmlTag   = regexp.MustCompile(`<[^>]*>`)
	wsRun     = regexp.MustCompile(`\s+`)
	leadingNum = regexp.MustCompile(`^[\d¼½¾⅓⅔⅛⅜⅝⅞.,/\s-]+`)
	leadingUnit = regexp.MustCompile(`(?i)^(grams?|g|kg|kilograms?|oz|ounces?|ml|milliliters?|l|liters?|lb|lbs|pounds?|cups?|tbsps?|tablespoons?|tsps?|teaspoons?|pinch|pinches|cloves?|cans?|sticks?|sprigs?)\b\.?\s+`)

	boilerplate = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\bjump to (recipe|video)\b`),
		regexp.MustCompile(`(?i)\bprint (recipe|this recipe)\b`),
		regexp.MustCompile(`(?i)this post may contain affiliate links[^.]*\.?`),
		regexp.MustCompile(`(?i)as an amazon associate[^.]*\.?`),
		regexp.MustCompile(`(?i)\bpin (this )?recipe\b`),
	}
)

// cleanDescription strips HTML, decodes entities, removes common blog boilerplate, collapses
// whitespace and caps overly long descriptions at a sentence boundary.
func cleanDescription(s, mode string) string {
	switch mode {
	case "blank":
		return ""
	case "raw":
		return strings.TrimSpace(s)
	}
	s = htmlTag.ReplaceAllString(s, " ")
	s = html.UnescapeString(s)
	for _, re := range boilerplate {
		s = re.ReplaceAllString(s, " ")
	}
	s = strings.TrimSpace(wsRun.ReplaceAllString(s, " "))
	if len(s) > 600 {
		s = s[:600]
		if i := strings.LastIndexAny(s, ".!?"); i > 200 {
			s = s[:i+1]
		}
		s = strings.TrimSpace(s)
	}
	return s
}

// prepWords are leading adjectives/participles that describe preparation, not the food itself.
// Deliberately conservative: meaning-bearing words like "ground", "dried", "smoked" and colour/
// variety words are NOT included, so "ground coriander" and "red onion" survive intact.
var prepWords = map[string]bool{
	"finely": true, "freshly": true, "coarsely": true, "thinly": true, "roughly": true,
	"lightly": true, "fresh": true, "chopped": true, "diced": true, "minced": true,
	"sliced": true, "grated": true, "shredded": true, "crushed": true, "peeled": true,
	"halved": true, "quartered": true, "cubed": true, "drained": true, "rinsed": true,
	"trimmed": true, "toasted": true, "beaten": true, "softened": true, "melted": true,
	"packed": true, "sifted": true, "divided": true, "large": true, "medium": true,
	"small": true, "ripe": true,
}

// cleanFoodName normalizes a (possibly mangled) food name and returns any text that should
// move to the ingredient note. Handles the failure modes the user reported:
//   "(1.75 ounces; 50g) sugar"   -> "sugar"            note: "1.75 ounces; 50g"
//   "sugar or brown sugar"       -> "sugar"            note: "or brown sugar"
//   "finely chopped white onion" -> "white onion"
//   "50g sugar"                  -> "sugar"
func cleanFoodName(name string) (string, string) {
	s := strings.TrimSpace(name)
	var notes []string

	// 1. Pull parenthetical groups out into notes.
	for {
		i := strings.Index(s, "(")
		if i < 0 {
			break
		}
		rest := s[i+1:]
		j := strings.Index(rest, ")")
		if j < 0 {
			if inner := strings.TrimSpace(rest); inner != "" {
				notes = append(notes, inner)
			}
			s = strings.TrimSpace(s[:i])
			break
		}
		if inner := strings.TrimSpace(rest[:j]); inner != "" {
			notes = append(notes, inner)
		}
		s = strings.TrimSpace(s[:i] + " " + rest[j+1:])
	}

	// 2. Resolve "X or Y" -> X, keeping the alternative as a note.
	if idx := strings.Index(strings.ToLower(s), " or "); idx >= 0 {
		if alt := strings.TrimSpace(s[idx+4:]); alt != "" {
			notes = append(notes, "or "+alt)
		}
		s = strings.TrimSpace(s[:idx])
	}

	// 2b. Split a trailing prep clause: "pork belly, cut into strips" -> food "pork belly",
	// note "cut into strips". The full text is always preserved in original_text + note.
	if i := strings.Index(s, ","); i >= 0 {
		if rest := strings.TrimSpace(s[i+1:]); rest != "" {
			notes = append(notes, rest)
		}
		s = strings.TrimSpace(s[:i])
	}

	// 3. Strip stacked leading measures/units (e.g. "15-ounce cans chickpeas" -> "chickpeas").
	for {
		before := s
		s = leadingNum.ReplaceAllString(s, "")
		s = leadingUnit.ReplaceAllString(s, "")
		if s == before {
			break
		}
	}

	// 4. Strip leading preparation adjectives.
	s = stripLeadingPrep(s)

	// 5. Tidy.
	s = strings.TrimSpace(wsRun.ReplaceAllString(s, " "))
	s = strings.Trim(s, " ,.;:-")

	return s, strings.Join(notes, "; ")
}

func stripLeadingPrep(s string) string {
	words := strings.Fields(s)
	i := 0
	for i < len(words)-1 { // keep at least one word
		w := strings.ToLower(strings.Trim(words[i], ",.;:"))
		if prepWords[w] {
			i++
			continue
		}
		break
	}
	return strings.Join(words[i:], " ")
}
