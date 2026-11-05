package enrich

import (
	"math"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// ParseLine is the shared ingredient-line parser (a port of cookbook-import push.py
// parse_ingredient, plus the cases the web importers trip on: mojibake, U+2044 fraction slash,
// "plus 1 Tbsp." compounds and "A pinch of ..." leads). It is used by CleanRows to re-parse rows
// that the original importer got wrong; rows it doesn't flag are never re-parsed.
type Parsed struct {
	Amount float64
	Unit   string
	Food   string // "" when the line has no food (e.g. "5 tablespoons")
	Note   string
}

var vulgar = map[rune]float64{
	'¼': .25, '½': .5, '¾': .75, '⅓': 1.0 / 3, '⅔': 2.0 / 3,
	'⅛': .125, '⅜': .375, '⅝': .625, '⅞': .875,
	'⅕': .2, '⅖': .4, '⅗': .6, '⅘': .8, '⅙': 1.0 / 6, '⅚': 5.0 / 6,
}

const vulgarClass = "¼½¾⅓⅔⅛⅜⅝⅞⅕⅖⅗⅘⅙⅚"

// unitWords: push.py UNITS (so the two importers agree on what a unit is).
var unitWords = setOf("tsp", "tsps", "teaspoon", "teaspoons", "tbsp", "tbsps", "tbs", "tablespoon", "tablespoons",
	"cup", "cups", "oz", "ounce", "ounces", "lb", "lbs", "pound", "pounds", "g", "gram", "grams", "kg", "ml",
	"l", "liter", "liters", "litre", "litres", "quart", "quarts", "qt", "pint", "pints",
	"gallon", "gallons", "clove", "cloves", "can", "cans", "package", "packages", "pinch",
	"pinches", "dash", "dashes", "handful", "handfuls", "sprig", "sprigs", "bunch", "bunches", "stalk",
	"stalks", "slice", "slices", "piece", "pieces", "head", "heads", "stick", "sticks",
	"scoop", "scoops", "drop", "drops", "block", "blocks", "bag", "bags", "jar", "jars", "box",
	"batch", "batches", "recipe")

// adjectives: push.py ADJECTIVES (+ a few seen as stand-alone foods in the library).
var adjectives = setOf("soft", "ripe", "firm", "extra-firm", "extrafirm", "large", "small",
	"medium", "fresh", "frozen", "raw", "dried", "whole", "ground", "chopped",
	"minced", "sliced", "diced", "packed", "cooked", "uncooked", "warm", "cold",
	"hot", "plain", "unsweetened", "sweetened", "organic", "pitted", "peeled",
	"seedless", "thick", "thin", "heaping", "scant", "lightly", "loosely",
	"finely", "roughly", "coarsely", "freshly", "very", "super", "quality",
	"unsalted", "salted", "toasted", "roasted", "smooth", "creamy", "crunchy",
	"boiling", "lukewarm", "chilled", "melted", "softened", "divided", "optional",
	"unflavored", "unflavoured", "low-sodium", "reduced-sodium", "full-fat", "light", "heaped", "level")

var (
	amountRe  = regexp.MustCompile(`^\s*(\d+\s+\d+/\d+|\d+/\d+|\d+(?:\.\d+)?\s*[` + vulgarClass + `]|\d+(?:\.\d+)?|[` + vulgarClass + `])\s*`)
	rangeRe   = regexp.MustCompile(`(?i)^\s*(?:to|or|[-–—])\s*[\d./` + vulgarClass + `]+\s*`)
	parenRe   = regexp.MustCompile(`\(([^()\[\]]*)\)`)
	brackRe   = regexp.MustCompile(`\[([^()\[\]]*)\]`)
	unitRngRe = regexp.MustCompile(`^([A-Za-z]+)\.?\s*[–—-]\s*[\d./` + vulgarClass + `]+\s*([A-Za-z]+)?\.?\s+`)
	plusRe    = regexp.MustCompile(`(?i)^\s*(\+|plus|and)\s*([\d./` + vulgarClass + `]+\s*[A-Za-z]+\.?)\s*`)
	fracSpRe  = regexp.MustCompile(`(\d)\s*/\s*(\d)`)
	leadRe    = regexp.MustCompile(`(?i)^(?:of|the|plus)\s+`)
	articleRe = regexp.MustCompile(`(?i)^((?:a|an|one)\s+(?:(?:few|couple)\s+(?:of\s+)?)?(?:(?:big|small|generous|good|large)\s+)?(?:pinch(?:es)?|dash(?:es)?|handfuls?|splash(?:es)?|sprinkl(?:e|ing)|drizzle|few|couple|squeeze|knob|bunch|sprigs?|glug|little|bit|drops?))\s+(?:of\s+)?`)
	orRe      = regexp.MustCompile(`\s+or\s+`)
	nonAlpha  = regexp.MustCompile(`[^a-z-]`)
)

func setOf(xs ...string) map[string]bool {
	m := make(map[string]bool, len(xs))
	for _, x := range xs {
		m[x] = true
	}
	return m
}

// NormalizeText repairs encoding damage before parsing: cp1252 double-decoded UTF-8
// ("jalapeÃ±o" -> "jalapeño", "3Â½" -> "3½"), the U+2044 fraction slash ("1⁄4" -> "1/4"),
// and non-breaking spaces.
func NormalizeText(s string) string {
	s = FixMojibake(s)
	s = strings.ReplaceAll(s, "⁄", "/")
	s = strings.ReplaceAll(s, " ", " ")
	return fracSpRe.ReplaceAllString(s, "$1/$2") // "1 / 6" -> "1/6"
}

var cp1252 = map[rune]byte{
	'€': 0x80, '‚': 0x82, 'ƒ': 0x83, '„': 0x84, '…': 0x85, '†': 0x86, '‡': 0x87, 'ˆ': 0x88, '‰': 0x89,
	'Š': 0x8A, '‹': 0x8B, 'Œ': 0x8C, 'Ž': 0x8E, '‘': 0x91, '’': 0x92, '“': 0x93, '”': 0x94, '•': 0x95,
	'–': 0x96, '—': 0x97, '˜': 0x98, '™': 0x99, 'š': 0x9A, '›': 0x9B, 'œ': 0x9C, 'ž': 0x9E, 'Ÿ': 0x9F,
}

func cpByte(r rune) (byte, bool) {
	if r < 0x100 {
		return byte(r), true
	}
	b, ok := cp1252[r]
	return b, ok
}

// FixMojibake re-decodes runs that are UTF-8 bytes mis-read as cp1252/latin-1. Each candidate run
// (a lead byte C2-F4 followed by continuation bytes) is only replaced when it decodes to exactly
// one valid rune, so legitimate accented text is left alone.
func FixMojibake(s string) string {
	if !strings.ContainsAny(s, "ÂÃâÅÆÐ") {
		return s
	}
	rs := []rune(s)
	var b strings.Builder
	for i := 0; i < len(rs); {
		lead, ok := cpByte(rs[i])
		n := 0
		switch {
		case ok && lead >= 0xC2 && lead <= 0xDF:
			n = 2
		case ok && lead >= 0xE0 && lead <= 0xEF:
			n = 3
		case ok && lead >= 0xF0 && lead <= 0xF4:
			n = 4
		}
		if n > 0 && i+n <= len(rs) {
			buf := []byte{lead}
			good := true
			for k := 1; k < n; k++ {
				c, ok := cpByte(rs[i+k])
				if !ok || c < 0x80 || c > 0xBF {
					good = false
					break
				}
				buf = append(buf, c)
			}
			if good {
				if r, size := utf8.DecodeRune(buf); r != utf8.RuneError && size == n {
					b.WriteRune(r)
					i += n
					continue
				}
			}
		}
		b.WriteRune(rs[i])
		i++
	}
	return b.String()
}

func toNumber(tok string) float64 {
	tok = strings.TrimSpace(tok)
	if m := regexp.MustCompile(`^(\d+)\s+(\d+)/(\d+)$`).FindStringSubmatch(tok); m != nil {
		a, _ := strconv.Atoi(m[1])
		b, _ := strconv.Atoi(m[2])
		c, _ := strconv.Atoi(m[3])
		if c == 0 {
			return 0
		}
		return float64(a) + float64(b)/float64(c)
	}
	if m := regexp.MustCompile(`^(\d+)/(\d+)$`).FindStringSubmatch(tok); m != nil {
		b, _ := strconv.Atoi(m[1])
		c, _ := strconv.Atoi(m[2])
		if c == 0 {
			return 0
		}
		return float64(b) / float64(c)
	}
	rs := []rune(tok)
	if len(rs) > 0 {
		if v, ok := vulgar[rs[len(rs)-1]]; ok {
			whole := strings.TrimSpace(string(rs[:len(rs)-1]))
			if whole == "" {
				return v
			}
			if f, err := strconv.ParseFloat(whole, 64); err == nil {
				return f + v
			}
		}
	}
	f, err := strconv.ParseFloat(tok, 64)
	if err != nil {
		return 0
	}
	return f
}

func isAdjective(w string) bool {
	return adjectives[nonAlpha.ReplaceAllString(strings.ToLower(w), "")]
}

// IsUnitWord reports whether w (any case, trailing period ok) is a unit word.
func IsUnitWord(w string) bool {
	w = strings.ToLower(strings.TrimRight(strings.TrimSpace(w), "."))
	return unitWords[w] || isKnownUnit(w)
}

// ParseLine parses one ingredient line.
func ParseLine(line string) Parsed {
	text := NormalizeText(strings.TrimSpace(line))
	var notes []string
	amount := 0.0
	if m := amountRe.FindStringSubmatchIndex(text); m != nil {
		amount = toNumber(text[m[2]:m[3]])
		tok := strings.TrimSpace(text[m[2]:m[3]])
		text = text[m[1]:]
		if r := rangeRe.FindStringIndex(text); r != nil {
			notes = append(notes, tok+" "+strings.TrimSpace(text[:r[1]]))
			text = text[r[1]:]
		}
	}
	// "A pinch of sea salt" -> food "sea salt", note "a pinch"
	if amount == 0 {
		if m := articleRe.FindStringSubmatchIndex(text); m != nil {
			notes = append(notes, strings.ToLower(text[m[2]:m[3]]))
			text = text[m[1]:]
		}
	}
	// parentheticals/brackets -> note, innermost first
	for {
		prev := text
		text = parenRe.ReplaceAllStringFunc(text, func(s string) string {
			notes = append(notes, strings.TrimSpace(s[1:len(s)-1]))
			return " "
		})
		text = brackRe.ReplaceAllStringFunc(text, func(s string) string {
			notes = append(notes, strings.TrimSpace(s[1:len(s)-1]))
			return " "
		})
		if text == prev {
			break
		}
	}
	text = strings.NewReplacer("(", " ", ")", " ", "[", " ", "]", " ").Replace(text)
	text = strings.Join(strings.Fields(text), " ")

	unit := ""
	if m := unitRngRe.FindStringSubmatchIndex(text); m != nil && unitWords[strings.ToLower(text[m[2]:m[3]])] {
		unit = text[m[2]:m[3]]
		notes = append(notes, strings.TrimSpace(text[:m[1]]))
		text = text[m[1]:]
	}
	if unit == "" {
		parts := strings.Fields(text)
		if len(parts) > 0 && unitWords[strings.ToLower(strings.TrimRight(parts[0], "."))] {
			unit = strings.TrimRight(parts[0], ".")
			text = strings.Join(parts[1:], " ")
		}
	}
	if m := plusRe.FindStringSubmatchIndex(text); m != nil {
		notes = append(notes, "plus "+strings.TrimSpace(text[m[4]:m[5]]))
		text = text[m[1]:]
	}
	text = strings.TrimLeft(text, " .")

	food := text
	if strings.Contains(text, ",") {
		segs := strings.Split(text, ",")
		for i := range segs {
			segs[i] = strings.TrimSpace(segs[i])
		}
		k := 0
		for k < len(segs)-1 && segs[k] != "" && allWords(segs[k], isAdjective) {
			notes = append(notes, segs[k])
			k++
		}
		food = segs[k]
		notes = append(notes, segs[k+1:]...)
	}
	if len(food) > 28 {
		if m := orRe.FindStringIndex(food); m != nil {
			head := food[:m[0]]
			headAdj := head != "" && allWords(head, func(w string) bool { return isAdjective(w) || isDigits(w) })
			if !headAdj && len(head) >= 8 {
				notes = append(notes, "or "+strings.TrimSpace(food[m[1]:]))
				food = head
			}
		}
	}
	food = leadRe.ReplaceAllString(strings.TrimSpace(food), "")
	food = strings.Trim(food, " .;:,")

	var kept []string
	for _, n := range notes {
		if n = strings.TrimSpace(n); n != "" {
			kept = append(kept, n)
		}
	}
	return Parsed{Amount: math.Round(amount*10000) / 10000, Unit: unit, Food: food, Note: strings.Join(kept, "; ")}
}

func allWords(s string, f func(string) bool) bool {
	ws := strings.Fields(s)
	if len(ws) == 0 {
		return false
	}
	for _, w := range ws {
		if !f(w) {
			return false
		}
	}
	return true
}

func isDigits(w string) bool {
	if w == "" {
		return false
	}
	for _, r := range w {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
