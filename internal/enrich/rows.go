package enrich

import (
	"regexp"
	"strings"
	"unicode"
)

// Row is one ingredient row as an importer (or Tandoor) stored it. Group is the step (or section)
// the row belongs to: merges and folds never cross groups. Loose marks a group whose stored order
// can't be trusted (tied/missing order values), so adjacency-based fixes there are report-only.
type Row struct {
	Amount       float64 `json:"amount"`
	Unit         string  `json:"unit"`
	Food         string  `json:"food"`
	Note         string  `json:"note"`
	OriginalText string  `json:"original_text"`
	IsHeader     bool    `json:"is_header"`
	Group        int     `json:"group"`
	Loose        bool    `json:"loose,omitempty"`
}

// OutRow is a cleaned row. From lists the input indices it came from (one, or two for a merge).
type OutRow struct {
	Row
	From  []int    `json:"from"`
	Kinds []string `json:"kinds,omitempty"`
}

// Fix records one change CleanRows found. Apply=false fixes are reported but not made (risky or
// report-only kinds, or adjacency fixes in a Loose group); After is nil for a drop.
type Fix struct {
	Kind   string `json:"kind"`
	Rows   []int  `json:"rows"`
	Before []Row  `json:"before"`
	After  *Row   `json:"after"`
	Risk   string `json:"risk"` // safe | risky | report
	Apply  bool   `json:"apply"`
	Reason string `json:"reason,omitempty"`
}

type CleanOptions struct {
	ApplyRisky bool // also apply risky kinds (yield-line / not-ingredient drops)
}

// Fix kinds.
const (
	KMojibake       = "mojibake"
	KFractionSlash  = "fraction-slash"
	KHeader         = "header"
	KLabelContent   = "label-content"
	KNotIngredient  = "not-ingredient"
	KYieldLine      = "yield-line"
	KBareNumber     = "bare-number"
	KLeadOfPlus     = "lead-of-plus"
	KUnitFood       = "unit-food"
	KModifierFood   = "modifier-food"
	KArticleLead    = "article-lead"
	KMeasureFood    = "measure-in-food"
	KSplitMerge     = "split-measure-merge"
	KUnpaired       = "unpaired-measure"
	KOrphan         = "orphan-modifier"
	KUnresolved     = "unresolved"
	KAmountMismatch = "amount-mismatch"
)

var (
	nutritionRe = regexp.MustCompile(`(?i)^\s*(calories|kcal|fats?|total fat|saturated fat|carbs|net carbs|carbohydrates?|protein|fib(er|re)|sugars?|sodium|cholesterol|notes?|tips?|nutrition( facts| info)?)\s*:`)
	yieldLeadRe = regexp.MustCompile(`(?i)^\s*(approximately|approx\.?|about|makes|yields?|serves)\b`)
	bareNumRe   = regexp.MustCompile(`^\s*[\d./` + vulgarClass + `\s-]+\s*$`)
	labelRe     = regexp.MustCompile(`^\s*([A-Za-z][A-Za-z '&/-]{0,30}?)\s*:\s*(\S.*)$`)
	foodLeadRe  = regexp.MustCompile(`(?i)^\s*(of|plus|the|and|\+|\.)(\s|$|\.)`)
	articleFood = regexp.MustCompile(`(?i)^(a|an)\s+(few|couple|big|small|generous|good|pinch|dash|handful|splash|sprinkl|drizzle|squeeze|knob|little|bit)`)
	digitLead   = regexp.MustCompile(`^[\d` + vulgarClass + `]`)
	orphanSet   = setOf("to taste", "for garnish", "plus more", "as needed", "or to taste", "plus more to taste",
		"at room temperature", "room temperature", "for serving", "plus extra", "or more", "more to taste")
)

// CleanRows finds and fixes importer damage in a recipe's ingredient rows. Rows it doesn't flag are
// passed through untouched (never re-parsed).
func CleanRows(rows []Row, opts CleanOptions) ([]OutRow, []Fix) {
	type st struct {
		row     Row
		kinds   []string
		drop    bool
		flagged bool
		from    []int
	}
	work := make([]st, len(rows))
	var fixes []Fix
	for i, r := range rows {
		work[i] = st{row: r, from: []int{i}}
	}
	add := func(f Fix) Fix {
		if f.Risk == "risky" && opts.ApplyRisky {
			f.Apply = true
		}
		fixes = append(fixes, f)
		return f
	}

	// Pass 1: per-row fixes.
	for i := range work {
		r := rows[i]
		if r.IsHeader {
			continue
		}
		nr, kind, drop, risk, reason := fixRow(r)
		if kind == "" {
			continue
		}
		work[i].flagged = true
		f := Fix{Kind: kind, Rows: []int{i}, Before: []Row{r}, Risk: risk, Apply: risk == "safe", Reason: reason}
		if !drop && kind != KUnresolved && kind != KBareNumber && kind != KAmountMismatch {
			cp := nr
			f.After = &cp
		}
		f = add(f)
		if f.Apply {
			if drop {
				work[i].drop = true
			} else {
				work[i].row = nr
			}
			work[i].kinds = append(work[i].kinds, kind)
		}
	}

	// Pass 2: structural fixes across adjacent rows in the same group.
	live := func(j int) bool { return !work[j].drop && !work[j].row.IsHeader }
	for i := range work {
		if !live(i) || work[i].flagged || !isMeasureOnly(work[i].row) {
			continue
		}
		r := work[i].row
		j := -1
		for k := i + 1; k < len(work); k++ {
			if work[k].drop {
				continue
			}
			if work[k].row.Group == r.Group {
				j = k
			}
			break
		}
		if j < 0 || !live(j) || work[j].row.Amount != 0 || work[j].row.Unit != "" || !CleanFood(work[j].row.Food) {
			add(Fix{Kind: KUnpaired, Rows: []int{i}, Before: []Row{rows[i]}, Risk: "report",
				Reason: "measure with no food and no food-only row after it"})
			continue
		}
		p := ParseLine(r.OriginalText)
		nxt := work[j].row
		merged := nxt
		merged.Amount, merged.Unit = p.Amount, strings.ToLower(p.Unit)
		merged.Note = joinNote(p.Note, nxt.Note)
		if mod := strings.TrimSpace(p.Food); mod != "" {
			if allWords(strings.ToLower(mod), func(w string) bool { return prepWords[strings.ToLower(w)] }) {
				merged.Note = joinNote(strings.ToLower(mod), merged.Note) // "3 tbsp melted" + "butter"
			} else {
				merged.Food = caseLike(mod+" "+nxt.Food, nxt.Food) // "1½ cups low-sodium" + "vegetable broth"
			}
		}
		merged.OriginalText = strings.TrimSpace(NormalizeText(r.OriginalText) + " " + nxt.OriginalText)
		f := Fix{Kind: KSplitMerge, Rows: []int{i, j}, Before: []Row{rows[i], rows[j]}, After: &merged, Risk: "safe", Apply: true}
		if r.Loose || nxt.Loose {
			f.Apply, f.Risk, f.Reason = false, "report", "stored order in this step is not trustworthy"
		}
		f = add(f)
		if f.Apply {
			work[j].row = merged
			work[j].from = append([]int{i}, work[j].from...)
			work[j].kinds = append(work[j].kinds, KSplitMerge)
			work[i].drop = true
		}
	}
	for i := range work {
		if !live(i) || work[i].flagged || !isOrphanModifier(work[i].row) {
			continue
		}
		p := -1
		for k := i - 1; k >= 0; k-- {
			if work[k].drop {
				continue
			}
			if work[k].row.Group == work[i].row.Group && !work[k].row.IsHeader {
				p = k
			}
			break
		}
		text := strings.Trim(NormalizeText(work[i].row.OriginalText), " ,;.")
		if text == "" {
			text = strings.Trim(work[i].row.Food, " ,;.")
		}
		if p < 0 {
			add(Fix{Kind: KOrphan, Rows: []int{i}, Before: []Row{rows[i]}, Risk: "report", Reason: "no ingredient before it in the same step"})
			continue
		}
		prev := work[p].row
		prev.Note = joinNote(prev.Note, text)
		prev.OriginalText = strings.TrimSpace(prev.OriginalText + ", " + text)
		f := Fix{Kind: KOrphan, Rows: []int{p, i}, Before: []Row{work[p].row, rows[i]}, After: &prev, Risk: "safe", Apply: true}
		if work[i].row.Loose {
			f.Apply, f.Risk, f.Reason = false, "report", "stored order in this step is not trustworthy"
		}
		f = add(f)
		if f.Apply {
			work[p].row = prev
			work[p].from = append(work[p].from, i)
			work[p].kinds = append(work[p].kinds, KOrphan)
			work[i].drop = true
		}
	}

	out := make([]OutRow, 0, len(work))
	for _, w := range work {
		if w.drop {
			continue
		}
		out = append(out, OutRow{Row: w.row, From: w.from, Kinds: w.kinds})
	}
	return out, fixes
}

// fixRow classifies a single non-header row. kind == "" means the row is clean.
func fixRow(r Row) (nr Row, kind string, drop bool, risk, reason string) {
	nr = r
	ot := NormalizeText(r.OriginalText)
	otTrim := strings.TrimSpace(ot)
	foodL := strings.ToLower(strings.TrimSpace(r.Food))
	noMeasure := r.Amount == 0 && strings.TrimSpace(r.Unit) == ""

	if nutritionRe.MatchString(otTrim) {
		return r, KNotIngredient, true, "risky", "nutrition or note line, not an ingredient"
	}
	if otTrim != "" && bareNumRe.MatchString(otTrim) {
		return r, KBareNumber, false, "report", "bare number (often a leaked SERVES n); left alone"
	}
	if otTrim != "" && !hasLower(otTrim) && hasDigit(otTrim) {
		if yieldLeadRe.MatchString(otTrim) || ParseLine(otTrim).Food == "" {
			return r, KYieldLine, true, "risky", "yield line (e.g. APPROXIMATELY 1 CUP)"
		}
	}
	if noMeasure && looksHeader(r.Food, otTrim) {
		h := r
		h.IsHeader, h.Food, h.Amount, h.Unit = true, "", 0, ""
		h.Note = strings.TrimSpace(strings.TrimRight(otOr(otTrim, r.Food), ":"))
		h.OriginalText = otTrim
		return h, KHeader, false, "safe", ""
	}
	if m := labelRe.FindStringSubmatch(otTrim); m != nil {
		label, content := strings.TrimSpace(m[1]), strings.TrimSpace(m[2])
		labelL := strings.ToLower(label)
		if foodL == labelL || strings.Contains(foodL, ":") || strings.HasPrefix(foodL, "optional") || strings.HasPrefix(foodL, labelL+" ") || foodL == "" {
			p := ParseLine(content)
			n := r
			n.OriginalText = otTrim
			switch {
			case CleanFood(p.Food) && strings.ToLower(p.Food) != labelL:
				pf, pn := reparsed(p)
				n.Amount, n.Unit, n.Food = p.Amount, strings.ToLower(p.Unit), caseLike(pf, r.Food)
				n.Note = joinNote(labelL, pn)
			case !strings.HasPrefix(labelL, "optional") && !strings.HasPrefix(labelL, "for ") && (p.Amount > 0 || p.Unit != ""):
				// "Cinnamon: 1½ tsp ground" -> food cinnamon, note ground
				n.Amount, n.Unit, n.Food = p.Amount, strings.ToLower(p.Unit), caseLike(label, r.Food)
				n.Note = joinNote(p.Food, p.Note)
			default:
				return r, KUnresolved, false, "report", "label line whose content didn't parse to a food"
			}
			if CleanFood(n.Food) && (n.Food != r.Food || n.Amount != r.Amount || n.Unit != r.Unit) {
				return n, KLabelContent, false, "safe", ""
			}
		}
	}

	// Re-parse family: the stored food is visibly wrong.
	bad := ""
	switch {
	case foodLeadRe.MatchString(foodL):
		bad = KLeadOfPlus
	case articleFood.MatchString(foodL):
		bad = KArticleLead
	case foodL != "" && IsUnitWord(foodL) && moreWordsThanMeasure(otTrim):
		bad = KUnitFood
	case foodL != "" && allWords(foodL, isAdjective) && moreWordsThanMeasure(otTrim):
		bad = KModifierFood
	case measureInFood(r.Food) || strings.Contains(r.Food, "\u2044") || strings.HasPrefix(foodL, "/"):
		bad = KMeasureFood
		if strings.Contains(r.OriginalText, "⁄") {
			bad = KFractionSlash
		}
	}
	if bad != "" {
		if isMeasureOnly(r) {
			return r, "", false, "", "" // pass 2 merges it with the next row
		}
		p := ParseLine(otTrim)
		if strings.EqualFold(p.Food, r.Food) {
			return r, "", false, "", "" // re-parse agrees with what's stored ("Pinch of cloves" -> cloves)
		}
		if !CleanFood(p.Food) {
			return r, KUnresolved, false, "report", bad + ": re-parse of the original text didn't give a clean food"
		}
		n := r
		pf, pn := reparsed(p)
		n.Amount, n.Unit, n.Food, n.Note = p.Amount, strings.ToLower(p.Unit), caseLike(pf, r.Food), pn
		n.OriginalText = otTrim
		return n, bad, false, "safe", ""
	}

	// Encoding damage only: repair the text fields in place.
	if fx := FixMojibake(r.Food); fx != r.Food || FixMojibake(r.Note) != r.Note || FixMojibake(r.OriginalText) != r.OriginalText {
		n := r
		n.Food, n.Note, n.OriginalText = FixMojibake(r.Food), FixMojibake(r.Note), FixMojibake(r.OriginalText)
		return n, KMojibake, false, "safe", ""
	}
	if strings.Contains(r.OriginalText, "⁄") && r.Amount == 0 {
		if p := ParseLine(otTrim); p.Amount > 0 && strings.EqualFold(p.Food, r.Food) {
			n := r
			n.Amount, n.OriginalText = p.Amount, otTrim
			if r.Unit == "" {
				n.Unit = strings.ToLower(p.Unit)
			}
			return n, KFractionSlash, false, "safe", ""
		}
	}

	// Report-only: stored amount disagrees with the original text.
	if r.Amount > 0 && otTrim != "" {
		if p := ParseLine(otTrim); p.Amount > 0 && strings.EqualFold(p.Food, r.Food) && absf(p.Amount-r.Amount) > 0.01 && strings.EqualFold(p.Unit, r.Unit) {
			return r, KAmountMismatch, false, "report", "stored amount differs from the original text"
		}
	}
	return r, "", false, "", ""
}

// measureInFood: the food itself starts with a measure ("2 tbsp oil"), not a name that happens to
// start with a number ("70% cacao chocolate", "5-Minute Granola").
func measureInFood(f string) bool {
	if !digitLead.MatchString(f) {
		return false
	}
	p := ParseLine(f)
	return p.Amount > 0 && p.Unit != ""
}

// CleanFood reports whether f is usable as a food name.
func CleanFood(f string) bool {
	f = strings.TrimSpace(f)
	fl := strings.ToLower(f)
	if f == "" || len(f) > 60 || strings.Contains(f, ":") {
		return false
	}
	if IsUnitWord(fl) || allWords(fl, isAdjective) || foodLeadRe.MatchString(fl) || measureInFood(f) || articleFood.MatchString(fl) || strings.HasPrefix(fl, "/") {
		return false
	}
	return FixMojibake(f) == f && !strings.Contains(f, "⁄")
}

// isMeasureOnly: the original text is just "amount [(paren)] unit" with no food ("5 tablespoons").
func isMeasureOnly(r Row) bool {
	ot := strings.TrimSpace(r.OriginalText)
	if ot == "" || r.IsHeader {
		return false
	}
	p := ParseLine(ot)
	if p.Unit == "" && p.Amount == 0 {
		return false
	}
	if p.Food == "" {
		return r.Food == "" || IsUnitWord(r.Food) || strings.EqualFold(r.Food, ot)
	}
	// "1½ cups low-sodium" + "vegetable broth": a measure plus a dangling modifier
	return allWords(strings.ToLower(p.Food), isAdjective) && strings.EqualFold(strings.TrimSpace(r.Food), p.Food)
}

func isOrphanModifier(r Row) bool {
	if r.Amount != 0 || r.Unit != "" || r.IsHeader {
		return false
	}
	ot := strings.TrimSpace(NormalizeText(r.OriginalText))
	if ot == "" {
		ot = r.Food
	}
	if strings.HasPrefix(ot, ",") || strings.HasPrefix(ot, ";") {
		return true
	}
	t := strings.ToLower(strings.Trim(ot, " ,;."))
	return t != "" && (orphanSet[t] || allWords(t, isAdjective))
}

func moreWordsThanMeasure(ot string) bool {
	p := ParseLine(ot)
	return p.Food != "" && len(strings.Fields(ot)) >= 2
}

// looksHeader: a section label rather than a food. Uses DetectHeader plus shouted ALL-CAPS lines
// (acronyms like TVP/MSG/BBQ excluded) and a bare "Optional".
func looksHeader(food, ot string) bool {
	if t := strings.TrimSpace(otOr(ot, food)); len(t) > 50 || strings.Count(t, " ") > 7 {
		return false // a sentence ("To serve, use a spatula to ...") is not a section label
	}
	if DetectHeader(food, ot) {
		return true
	}
	t := strings.TrimSpace(strings.TrimRight(otOr(ot, food), ":"))
	if strings.EqualFold(t, "optional") || strings.EqualFold(t, "optional toppings") || strings.EqualFold(t, "optional garnishes") {
		return true
	}
	return shoutedLine(t)
}

var headerAcronyms = setOf("BBQ", "AF", "TVP", "PB", "PBJ", "BLT", "GF", "DIY", "MSG", "CBD", "TLC", "BFF",
	"NYC", "LA", "NY", "AM", "PM", "OG", "XL", "ABC", "TSO", "CSA")

func shoutedLine(t string) bool {
	if t == "" || hasLower(t) || hasDigit(t) {
		return false
	}
	letters, real := 0, 0
	for _, w := range strings.Fields(t) {
		core := strings.Map(func(r rune) rune {
			if unicode.IsLetter(r) {
				return r
			}
			return -1
		}, w)
		letters += len([]rune(core))
		if core != "" && !headerAcronyms[core] {
			real++
		}
	}
	return letters >= 3 && real > 0
}

func otOr(ot, food string) string {
	if strings.TrimSpace(ot) != "" {
		return ot
	}
	return food
}

// reparsed turns a ParseLine result into food/note for a re-parsed row: leading prep words
// ("chopped cilantro") move to the note, like the import path's cleanFoodName.
func reparsed(p Parsed) (food, note string) {
	food = stripLeadingPrep(p.Food)
	note = p.Note
	if lead := strings.TrimSpace(strings.TrimSuffix(p.Food, food)); lead != "" {
		note = joinNote(strings.ToLower(lead), note)
	}
	return food, note
}

func caseLike(newFood, oldFood string) string {
	if oldFood == strings.ToLower(oldFood) {
		return strings.ToLower(newFood)
	}
	return newFood
}

func hasLower(s string) bool {
	for _, r := range s {
		if unicode.IsLower(r) {
			return true
		}
	}
	return false
}

func hasDigit(s string) bool {
	for _, r := range s {
		if unicode.IsDigit(r) {
			return true
		}
		if _, ok := vulgar[r]; ok {
			return true
		}
	}
	return false
}

func absf(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
