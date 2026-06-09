// Package reprocess improves recipes ALREADY in Tandoor: it communizes ingredients, drops parser-
// junk rows, fixes mis-parsed section headers, and (for the "everything in one step" shape, or when
// forced) re-allocates ingredients to the steps that use them via the shared component-aware
// allocator. It works on the raw recipe JSON map so no field is dropped except intended junk.
//
// Componentized recipes (ones with ingredient-section headers) are detected and reported as
// "componentized": their stored ingredient order is lost, so re-grouping them would SCRAMBLE the
// allocation — they need a re-import from source instead, and reprocess refuses to touch them.
package reprocess

import (
	"sort"
	"strings"

	"recipearr/internal/enrich"
)

type Options struct {
	Allocate, Communize, DropJunk, Force bool
}

func Default() Options { return Options{Allocate: true, Communize: true, DropJunk: true} }

type Result struct {
	Moved, Communized, Dropped, Headers int
	SkipReason                          string           // non-empty => not changed
	NewSteps                            []map[string]any // the steps payload to PATCH (nil if skipped)
}

// Changed reports whether applying this result would alter the recipe.
func (r Result) Changed() bool {
	return r.SkipReason == "" && (r.Moved+r.Communized+r.Dropped+r.Headers) > 0
}

// IsComponentized reports whether a recipe has ingredient-section headers (stored is_header rows or
// scraper-mis-parsed "For the …:" rows). Such recipes can't be safely re-allocated from stored state.
func IsComponentized(rec map[string]any) bool {
	for _, s := range toSlice(rec["steps"]) {
		sm, _ := s.(map[string]any)
		for _, ig := range toSlice(sm["ingredients"]) {
			im, _ := ig.(map[string]any)
			if boolOr(im["is_header"]) || isDetectedHeader(im) {
				return true
			}
		}
	}
	return false
}

// ProcessRecipe computes the reprocessed steps + change counts for one recipe map. It never mutates
// rec. SkipReason is set (and NewSteps nil) when the recipe is skipped.
func ProcessRecipe(rec map[string]any, opts Options) Result {
	if reason := hardSkip(rec); reason != "" {
		return Result{SkipReason: reason}
	}
	// Componentized recipes would be scrambled by re-allocation (lost order) — refuse when we'd
	// re-allocate. Cleaning-only (no allocate) is still safe, but the value is allocation, so skip.
	if opts.Allocate && IsComponentized(rec) {
		return Result{SkipReason: "componentized"}
	}
	steps, moved, communized, dropped, headers := buildSteps(rec, opts)
	return Result{Moved: moved, Communized: communized, Dropped: dropped, Headers: headers, NewSteps: steps}
}

func hardSkip(rec map[string]any) string {
	steps := toSlice(rec["steps"])
	if len(steps) == 0 {
		return "no steps"
	}
	total := 0
	for _, s := range steps {
		sm, _ := s.(map[string]any)
		if sm["file"] != nil {
			return "has step file"
		}
		if sm["step_recipe"] != nil {
			return "has sub-recipe step"
		}
		for _, ig := range toSlice(sm["ingredients"]) {
			if im, _ := ig.(map[string]any); !boolOr(im["is_header"]) {
				total++
			}
		}
	}
	if total == 0 {
		return "no ingredients"
	}
	return ""
}

func buildSteps(rec map[string]any, opts Options) (out []map[string]any, moved, cleaned, dropped, headered int) {
	stepsAny := toSlice(rec["steps"])
	stepMaps := make([]map[string]any, len(stepsAny))
	for i, s := range stepsAny {
		stepMaps[i], _ = s.(map[string]any)
	}

	stepsWithIng := 0
	for _, sm := range stepMaps {
		for _, ig := range toSlice(sm["ingredients"]) {
			if im, _ := ig.(map[string]any); !boolOr(im["is_header"]) {
				stepsWithIng++
				break
			}
		}
	}
	doAllocate := opts.Allocate && len(stepMaps) > 1 && (stepsWithIng <= 1 || opts.Force)

	type pending struct {
		patch map[string]any
		seq   int
	}
	perStep := make([][]pending, len(stepMaps))
	type movable struct {
		im     map[string]any
		origin int
		seq    int
	}
	var pool []movable

	instructions := make([]string, len(stepMaps))
	for i, sm := range stepMaps {
		instructions[i] = instruction(sm)
	}
	var fullIng []enrich.Ing
	seq := 0
	for i, sm := range stepMaps {
		for _, ig := range toSlice(sm["ingredients"]) {
			im, _ := ig.(map[string]any)
			isHeader := boolOr(im["is_header"]) || isDetectedHeader(im)
			fullIng = append(fullIng, enrich.Ing{
				Food:         communizedName(im, opts.Communize),
				OriginalText: strOr(im["original_text"]),
				Note:         strOr(im["note"]),
				IsHeader:     isHeader,
			})
			if isHeader {
				perStep[i] = append(perStep[i], pending{headerPatch(im), seq})
				if !boolOr(im["is_header"]) || strings.TrimSpace(foodName(im)) != "" {
					headered++
				}
			} else {
				pool = append(pool, movable{im, i, seq})
			}
			seq++
		}
	}

	var stepOf []int
	if doAllocate {
		stepOf = enrich.AssignSteps(fullIng, instructions)
	}
	for _, h := range pool {
		dst := h.origin
		if doAllocate {
			if s := stepOf[h.seq]; s >= 0 && s < len(stepMaps) {
				dst = s
			}
		}
		if dst != h.origin {
			moved++
		}
		p := ingredientPatch(h.im, opts.Communize, &cleaned)
		if opts.DropJunk && isJunkFood(foodNameOf(p)) {
			dropped++
			continue
		}
		perStep[dst] = append(perStep[dst], pending{p, h.seq})
	}

	out = make([]map[string]any, len(stepMaps))
	for i, sm := range stepMaps {
		ps := perStep[i]
		sort.SliceStable(ps, func(a, b int) bool { return ps[a].seq < ps[b].seq })
		ings := make([]map[string]any, len(ps))
		for k := range ps {
			ps[k].patch["order"] = k
			ings[k] = ps[k].patch
		}
		out[i] = stepPatch(sm, ings)
	}
	return out, moved, cleaned, dropped, headered
}

func isDetectedHeader(im map[string]any) bool {
	if !amountIsZero(im["amount"]) || strings.TrimSpace(unitName(im)) != "" {
		return false
	}
	t := strings.TrimSpace(strOr(im["original_text"]))
	if t == "" {
		t = strings.TrimSpace(foodName(im))
	}
	return enrich.DetectHeader(foodName(im), t)
}

func headerPatch(im map[string]any) map[string]any {
	label := strings.TrimRight(strings.TrimSpace(foodName(im)), ": ")
	if label == "" {
		label = strings.TrimRight(strings.TrimSpace(strOr(im["note"])), ": ")
	}
	if label == "" {
		label = strings.TrimRight(strings.TrimSpace(strOr(im["original_text"])), ": ")
	}
	return map[string]any{
		"food": nil, "unit": nil, "amount": 0, "note": label,
		"original_text": strOr(im["original_text"]), "is_header": true, "no_amount": true, "order": im["order"],
	}
}

func communizedName(im map[string]any, communize bool) string {
	f := foodName(im)
	if !communize || boolOr(im["is_header"]) {
		return f
	}
	nf, _, _ := enrich.CommunizeIngredient(f, unitName(im), nil)
	return nf
}

func ingredientPatch(im map[string]any, communize bool, cleaned *int) map[string]any {
	food := foodName(im)
	unit := unitName(im)
	note := strOr(im["note"])
	if communize && !boolOr(im["is_header"]) {
		nf, nu, extra := enrich.CommunizeIngredient(food, unit, nil)
		if nf != strings.TrimSpace(food) || nu != strings.TrimSpace(unit) {
			if cleaned != nil {
				*cleaned++
			}
		}
		food, unit = nf, nu
		note = joinNote(note, extra)
	}
	var unitVal any
	if strings.TrimSpace(unit) != "" {
		unitVal = map[string]any{"name": strings.TrimSpace(unit)}
	}
	return map[string]any{
		"food": map[string]any{"name": food}, "unit": unitVal, "amount": im["amount"], "note": note,
		"original_text": strOr(im["original_text"]), "is_header": boolOr(im["is_header"]),
		"no_amount": boolOr(im["no_amount"]), "order": im["order"],
	}
}

func stepPatch(sm map[string]any, ings []map[string]any) map[string]any {
	showTable := boolOr(sm["show_ingredients_table"])
	if len(ings) > 0 {
		showTable = true
	}
	return map[string]any{
		"id": sm["id"], "instruction": strOr(sm["instruction"]), "name": strOr(sm["name"]),
		"time": numOr(sm["time"]), "order": numOr(sm["order"]), "show_as_header": boolOr(sm["show_as_header"]),
		"show_ingredients_table": showTable, "ingredients": ings,
	}
}

// Counts returns a recipe's (ingredient, step) counts — for after-PATCH safety verification.
func Counts(rec map[string]any) (ings, steps int) {
	st := toSlice(rec["steps"])
	for _, s := range st {
		sm, _ := s.(map[string]any)
		ings += len(toSlice(sm["ingredients"]))
	}
	return ings, len(st)
}

var junkFoods = map[string]bool{
	"": true, "cup": true, "cups": true, "teaspoon": true, "teaspoons": true, "tsp": true,
	"tablespoon": true, "tablespoons": true, "tbsp": true, "ounce": true, "ounces": true, "oz": true,
	"pound": true, "pounds": true, "lb": true, "lbs": true, "gram": true, "grams": true, "g": true,
	"kg": true, "ml": true, "milliliter": true, "milliliters": true, "liter": true, "liters": true,
	"l": true, "quart": true, "quarts": true, "pint": true, "pints": true, "gallon": true, "gallons": true,
	"pinch": true, "dash": true,
}

func isJunkFood(name string) bool { return junkFoods[strings.ToLower(strings.TrimSpace(name))] }

func foodNameOf(p map[string]any) string {
	if fm, ok := p["food"].(map[string]any); ok {
		n, _ := fm["name"].(string)
		return n
	}
	return ""
}

func amountIsZero(v any) bool {
	switch x := v.(type) {
	case float64:
		return x == 0
	case string:
		s := strings.TrimSpace(x)
		return s == "" || s == "0" || s == "0.0" || s == "0.00" || s == "0.000"
	case nil:
		return true
	}
	return false
}

func toSlice(v any) []any { s, _ := v.([]any); return s }
func strOr(v any) string  { s, _ := v.(string); return s }
func boolOr(v any) bool   { b, _ := v.(bool); return b }
func numOr(v any) any {
	if v == nil {
		return 0
	}
	return v
}
func joinNote(a, b string) string {
	a, b = strings.TrimSpace(a), strings.TrimSpace(b)
	switch {
	case a == "":
		return b
	case b == "":
		return a
	default:
		return a + "; " + b
	}
}
func instruction(sm map[string]any) string { s, _ := sm["instruction"].(string); return s }
func foodName(im map[string]any) string {
	if f, ok := im["food"].(map[string]any); ok {
		n, _ := f["name"].(string)
		return n
	}
	return ""
}
func unitName(im map[string]any) string {
	if u, ok := im["unit"].(map[string]any); ok {
		n, _ := u["name"].(string)
		return n
	}
	return ""
}
