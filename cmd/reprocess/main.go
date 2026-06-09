// Command reprocess improves recipes ALREADY in Tandoor: it communizes their ingredients
// (folds bogus units like "guajillo"/"flour" into the food, cleans leaked weights/parentheticals
// /prep, resolves "X or Y"), drops parser-junk rows whose food is blank or a bare unit, and for
// recipes where every ingredient is piled into one step, re-allocates ingredients to the steps
// that use them (distributing duplicates across the steps that mention them).
//
// Safety: dry run unless -apply; works on the raw recipe JSON (no field dropped except intended
// junk); ingredient ids are recreated (foods re-link by name); is_header rows stay put and are
// never moved/communized/dropped; allocation only fires on the "unallocated" shape; recipes with
// step files or sub-recipe steps are skipped. Each recipe's pre-change JSON is saved to -backup,
// and after each PATCH ingredient/step counts are verified (allowing for intended junk drops).
// Use -restore <dir> to PATCH backed-up recipes back to their original state.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"recipearr/internal/enrich"
	"recipearr/internal/tandoor"
)

type procOpts struct {
	allocate, communize, dropJunk, forceShowTable bool
}

func main() {
	var (
		urlFlag    = flag.String("url", os.Getenv("TANDOOR_URL"), "Tandoor base URL")
		tokenFlag  = flag.String("token", os.Getenv("TANDOOR_TOKEN"), "Tandoor API token")
		idFlag     = flag.Int("id", 0, "process a single recipe id (0 = scan all)")
		copyFrom   = flag.Int("copyfrom", 0, "create a throwaway copy of this recipe id, print the new id, and exit")
		restoreDir = flag.String("restore", "", "PATCH every recipe-*.json in this dir back to its saved state, and exit")
		revertFile = flag.String("revert", "", "verbatim-restore recipes from this backup JSON array onto live (undo reprocessing), and exit")
		limit      = flag.Int("limit", 0, "max recipes to act on (0 = no limit)")
		apply      = flag.Bool("apply", false, "actually PATCH recipes (default: dry run)")
		allocate   = flag.Bool("allocate", true, "re-allocate ingredients to steps (unallocated recipes only)")
		communize  = flag.Bool("communize", true, "clean food names / fold bogus units")
		dropJunk   = flag.Bool("dropjunk", true, "drop ingredient rows whose food is blank or a bare unit")
		backupDir  = flag.String("backup", "", "directory to save each recipe's JSON before patching")
		verbose    = flag.Bool("v", false, "verbose per-recipe logging")
	)
	flag.Parse()

	if *urlFlag == "" || *tokenFlag == "" {
		fmt.Fprintln(os.Stderr, "set -url/-token or TANDOOR_URL/TANDOOR_TOKEN")
		os.Exit(2)
	}
	tc := tandoor.New(*urlFlag, *tokenFlag)
	ctx := context.Background()

	if *copyFrom != 0 {
		id, err := makeCopy(ctx, tc, *copyFrom)
		if err != nil {
			fmt.Fprintln(os.Stderr, "copy failed:", err)
			os.Exit(1)
		}
		fmt.Printf("created test copy: %d\n", id)
		return
	}

	if *restoreDir != "" {
		restore(ctx, tc, *restoreDir)
		return
	}

	if *revertFile != "" {
		runRevert(ctx, tc, *revertFile, *idFlag, *limit, *apply, *verbose)
		return
	}

	opts := procOpts{allocate: *allocate, communize: *communize, dropJunk: *dropJunk, forceShowTable: true}

	if *backupDir != "" {
		_ = os.MkdirAll(*backupDir, 0o755)
	}

	var ids []int
	if *idFlag != 0 {
		ids = []int{*idFlag}
	} else {
		var err error
		if ids, err = tc.ListRecipeIDs(ctx); err != nil {
			fmt.Fprintln(os.Stderr, "list recipes:", err)
			os.Exit(1)
		}
	}
	fmt.Printf("scanning %d recipe(s)  apply=%v allocate=%v communize=%v dropjunk=%v\n",
		len(ids), *apply, opts.allocate, opts.communize, opts.dropJunk)

	var acted, patched, skipped, failed, unsafe int
	for _, id := range ids {
		if *limit > 0 && acted >= *limit {
			break
		}
		raw, err := tc.GetRecipeRaw(ctx, id)
		if err != nil {
			fmt.Printf("[%d] GET failed: %v\n", id, err)
			failed++
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal(raw, &rec); err != nil {
			fmt.Printf("[%d] decode failed: %v\n", id, err)
			failed++
			continue
		}
		name, _ := rec["name"].(string)

		if reason := hardSkip(rec); reason != "" {
			if *verbose {
				fmt.Printf("[%d] skip (%s): %q\n", id, reason, name)
			}
			skipped++
			continue
		}

		steps, moved, cleaned, dropped, headered := buildSteps(rec, opts)
		if moved == 0 && cleaned == 0 && dropped == 0 && headered == 0 {
			if *verbose {
				fmt.Printf("[%d] no change: %q\n", id, name)
			}
			skipped++
			continue
		}
		acted++
		fmt.Printf("[%d] %q: moved %d, communized %d, dropped %d, headers %d%s\n", id, name, moved, cleaned, dropped, headered, tag(*apply))
		if !*apply {
			continue
		}

		if *backupDir != "" {
			_ = os.WriteFile(filepath.Join(*backupDir, fmt.Sprintf("recipe-%d.json", id)), raw, 0o644)
		}
		body, _ := json.Marshal(map[string]any{"steps": steps})
		if _, err := tc.PatchRecipeRaw(ctx, id, body); err != nil {
			fmt.Printf("[%d] PATCH FAILED: %v\n", id, err)
			failed++
			continue
		}
		if after, err := tc.GetRecipeRaw(ctx, id); err == nil {
			if d := countSafe(raw, after, dropped); d != "" {
				fmt.Printf("[%d] !! SAFETY: %s\n", id, d)
				unsafe++
			}
		}
		patched++
		time.Sleep(150 * time.Millisecond)
	}
	fmt.Printf("\ndone: acted=%d patched=%d skipped=%d failed=%d unsafe=%d\n", acted, patched, skipped, failed, unsafe)
	if unsafe > 0 || failed > 0 {
		os.Exit(1)
	}
}

// restore PATCHes each saved recipe back to its original structure (identity transform).
func restore(ctx context.Context, tc *tandoor.Client, dir string) {
	files, _ := filepath.Glob(filepath.Join(dir, "recipe-*.json"))
	identity := procOpts{} // all false: keep step assignment, no communize/drop, preserve show_table
	n := 0
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var rec map[string]any
		if json.Unmarshal(raw, &rec) != nil {
			continue
		}
		idf, _ := rec["id"].(float64)
		id := int(idf)
		steps, _, _, _, _ := buildSteps(rec, identity)
		body, _ := json.Marshal(map[string]any{"steps": steps})
		if _, err := tc.PatchRecipeRaw(ctx, id, body); err != nil {
			fmt.Printf("restore %d FAILED: %v\n", id, err)
			continue
		}
		fmt.Printf("restored %d\n", id)
		n++
		time.Sleep(120 * time.Millisecond)
	}
	fmt.Printf("restored %d recipe(s)\n", n)
}

// runRevert PATCHes each recipe in the backup array back onto live, verbatim (no transform) —
// undoing all reprocessing. Step ids are preserved so steps update in place.
func runRevert(ctx context.Context, tc *tandoor.Client, file string, onlyID, limit int, apply, verbose bool) {
	data, err := os.ReadFile(file)
	if err != nil {
		fmt.Fprintln(os.Stderr, "read:", err)
		os.Exit(1)
	}
	// PowerShell's Out-File -Encoding utf8 prepends a BOM that Go's JSON parser rejects.
	data = []byte(strings.TrimPrefix(string(data), "\ufeff"))
	var recipes []map[string]any
	if err := json.Unmarshal(data, &recipes); err != nil {
		fmt.Fprintln(os.Stderr, "parse:", err)
		os.Exit(1)
	}
	fmt.Printf("reverting from %s (%d recipes)  apply=%v\n", file, len(recipes), apply)
	var done, failed, mismatched int
	for _, rec := range recipes {
		if limit > 0 && done >= limit {
			break
		}
		idf, _ := rec["id"].(float64)
		id := int(idf)
		if id == 0 || (onlyID != 0 && id != onlyID) {
			continue
		}
		steps := revertSteps(rec)
		if len(steps) == 0 {
			continue
		}
		if !apply {
			fmt.Printf("[%d] would revert (%d steps)\n", id, len(steps))
			done++
			continue
		}
		body, _ := json.Marshal(map[string]any{"steps": steps})
		if _, err := tc.PatchRecipeRaw(ctx, id, body); err != nil {
			fmt.Printf("[%d] revert FAILED: %v\n", id, err)
			failed++
			continue
		}
		if after, err := tc.GetRecipeRaw(ctx, id); err == nil {
			bi, bs := countsMap(rec)
			ai, as := counts(after)
			if ai != bi || as != bs {
				fmt.Printf("[%d] !! mismatch ingredients %d->%d steps %d->%d\n", id, bi, ai, bs, as)
				mismatched++
			}
		}
		if verbose {
			fmt.Printf("[%d] reverted\n", id)
		}
		done++
		time.Sleep(120 * time.Millisecond)
	}
	fmt.Printf("\ndone: reverted=%d failed=%d mismatched=%d\n", done, failed, mismatched)
	if failed > 0 || mismatched > 0 {
		os.Exit(1)
	}
}

// revertSteps rebuilds a recipe's steps exactly as they were in the backup — original food/unit,
// order, is_header, step assignment — with no communize/allocation/header-detection. Ingredient
// ids are dropped (recreated, foods re-link by name); step ids are kept (update in place).
func revertSteps(rec map[string]any) []map[string]any {
	steps := toSlice(rec["steps"])
	out := make([]map[string]any, 0, len(steps))
	for _, s := range steps {
		sm, _ := s.(map[string]any)
		ings := make([]map[string]any, 0)
		for _, ig := range toSlice(sm["ingredients"]) {
			im, _ := ig.(map[string]any)
			var foodVal any
			if f, ok := im["food"].(map[string]any); ok {
				if n, _ := f["name"].(string); strings.TrimSpace(n) != "" {
					foodVal = map[string]any{"name": n}
				}
			}
			var unitVal any
			if u, ok := im["unit"].(map[string]any); ok {
				if n, _ := u["name"].(string); strings.TrimSpace(n) != "" {
					unitVal = map[string]any{"name": n}
				}
			}
			ings = append(ings, map[string]any{
				"food":          foodVal,
				"unit":          unitVal,
				"amount":        im["amount"],
				"note":          strOr(im["note"]),
				"original_text": strOr(im["original_text"]),
				"is_header":     boolOr(im["is_header"]),
				"no_amount":     boolOr(im["no_amount"]),
				"order":         im["order"],
			})
		}
		out = append(out, map[string]any{
			"id":                     sm["id"],
			"instruction":            strOr(sm["instruction"]),
			"name":                   strOr(sm["name"]),
			"time":                   numOr(sm["time"]),
			"order":                  numOr(sm["order"]),
			"show_as_header":         boolOr(sm["show_as_header"]),
			"show_ingredients_table": boolOr(sm["show_ingredients_table"]),
			"ingredients":            ings,
		})
	}
	return out
}

func countsMap(rec map[string]any) (ings, steps int) {
	st := toSlice(rec["steps"])
	for _, s := range st {
		sm, _ := s.(map[string]any)
		ings += len(toSlice(sm["ingredients"]))
	}
	return ings, len(st)
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

func buildSteps(rec map[string]any, opts procOpts) (out []map[string]any, moved, cleaned, dropped, headered int) {
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
	doAllocate := opts.allocate && len(stepMaps) > 1 && stepsWithIng <= 1

	// Each pending row carries its original flat position (seq) so headers stay interleaved
	// with their section's ingredients instead of bunching at the top.
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
	primary, primaryCount := 0, -1
	seq := 0
	for i, sm := range stepMaps {
		cnt := 0
		for _, ig := range toSlice(sm["ingredients"]) {
			im, _ := ig.(map[string]any)
			isHeader := boolOr(im["is_header"])
			if isHeader || isDetectedHeader(im) {
				perStep[i] = append(perStep[i], pending{headerPatch(im), seq})
				// Count as a change when newly detected, or when an existing header still
				// carries a food (the old buggy representation that needs clearing).
				if !isHeader || strings.TrimSpace(foodName(im)) != "" {
					headered++
				}
			} else {
				pool = append(pool, movable{im, i, seq})
				cnt++
			}
			seq++
		}
		if cnt > primaryCount {
			primaryCount, primary = cnt, i
		}
	}

	// Assign each ingredient to a step. Duplicates of the same food are spread across the steps
	// that mention it, in order (1st occurrence -> 1st matching step, 2nd -> 2nd, ...).
	placed := map[string]int{}
	for _, h := range pool {
		dst := h.origin
		if doAllocate {
			dst = primary
			if head := headWord(communizedName(h.im, opts.communize)); head != "" {
				var matches []int
				for i, sm := range stepMaps {
					if mentions(instruction(sm), head) {
						matches = append(matches, i)
					}
				}
				if len(matches) > 0 {
					k := placed[head]
					if k >= len(matches) {
						k = len(matches) - 1
					}
					dst = matches[k]
					placed[head]++
				}
			}
		}
		if dst != h.origin {
			moved++
		}
		p := ingredientPatch(h.im, opts.communize, &cleaned)
		if opts.dropJunk && isJunkFood(foodNameOf(p)) {
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
			ps[k].patch["order"] = k // renumber so display order matches the array order
			ings[k] = ps[k].patch
		}
		out[i] = stepPatch(sm, ings, opts.forceShowTable)
	}
	return out, moved, cleaned, dropped, headered
}

// isDetectedHeader spots a section header that a scraper mis-parsed as an ingredient: zero
// amount, no unit, and text that ends with ":" or starts with "for the " (e.g. "For the sauce:").
func isDetectedHeader(im map[string]any) bool {
	if !amountIsZero(im["amount"]) || strings.TrimSpace(unitName(im)) != "" {
		return false
	}
	t := strings.TrimSpace(strOr(im["original_text"]))
	if t == "" {
		t = strings.TrimSpace(foodName(im))
	}
	if t == "" {
		return false
	}
	return strings.HasSuffix(t, ":") || strings.HasPrefix(strings.ToLower(t), "for the ")
}

// headerPatch renders an ingredient row as a section header. Tandoor displays a header row's
// text from the NOTE field (food/amount/unit are ignored for headers), so the label goes there
// and food is cleared (no junk "For the …" foods).
func headerPatch(im map[string]any) map[string]any {
	label := strings.TrimRight(strings.TrimSpace(foodName(im)), ": ")
	if label == "" {
		label = strings.TrimRight(strings.TrimSpace(strOr(im["note"])), ": ")
	}
	if label == "" {
		label = strings.TrimRight(strings.TrimSpace(strOr(im["original_text"])), ": ")
	}
	return map[string]any{
		"food":          nil,
		"unit":          nil,
		"amount":        0,
		"note":          label,
		"original_text": strOr(im["original_text"]),
		"is_header":     true,
		"no_amount":     true,
		"order":         im["order"],
	}
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
		"food":          map[string]any{"name": food},
		"unit":          unitVal,
		"amount":        im["amount"],
		"note":          note,
		"original_text": strOr(im["original_text"]),
		"is_header":     boolOr(im["is_header"]),
		"no_amount":     boolOr(im["no_amount"]),
		"order":         im["order"],
	}
}

func stepPatch(sm map[string]any, ings []map[string]any, forceShowTable bool) map[string]any {
	showTable := boolOr(sm["show_ingredients_table"])
	if forceShowTable && len(ings) > 0 {
		showTable = true // a step that has ingredients must display them
	}
	return map[string]any{
		"id":                     sm["id"],
		"instruction":            strOr(sm["instruction"]),
		"name":                   strOr(sm["name"]),
		"time":                   numOr(sm["time"]),
		"order":                  numOr(sm["order"]),
		"show_as_header":         boolOr(sm["show_as_header"]),
		"show_ingredients_table": showTable,
		"ingredients":            ings,
	}
}

func makeCopy(ctx context.Context, tc *tandoor.Client, srcID int) (int, error) {
	raw, err := tc.GetRecipeRaw(ctx, srcID)
	if err != nil {
		return 0, err
	}
	var rec map[string]any
	if err := json.Unmarshal(raw, &rec); err != nil {
		return 0, err
	}
	steps, _, _, _, _ := buildSteps(rec, procOpts{}) // identity structure
	for _, s := range steps {
		delete(s, "id")
	}
	name, _ := rec["name"].(string)
	body, _ := json.Marshal(map[string]any{
		"name": name + " (reprocess test COPY)", "internal": true, "keywords": []any{}, "steps": steps,
	})
	resp, err := tc.PostRecipeRaw(ctx, body)
	if err != nil {
		return 0, err
	}
	var created struct {
		ID int `json:"id"`
	}
	if err := json.Unmarshal(resp, &created); err != nil {
		return 0, err
	}
	return created.ID, nil
}

// countSafe verifies the after-PATCH ingredient count equals before minus intended drops, and
// step count is unchanged. The per-recipe backup is the authoritative restore path.
func countSafe(beforeRaw, afterRaw []byte, droppedExpected int) string {
	bi, bs := counts(beforeRaw)
	ai, as := counts(afterRaw)
	if ai != bi-droppedExpected {
		return fmt.Sprintf("ingredient count %d -> %d (expected %d)", bi, ai, bi-droppedExpected)
	}
	if bs != as {
		return fmt.Sprintf("step count %d -> %d", bs, as)
	}
	return ""
}

func counts(raw []byte) (ings, steps int) {
	var rec map[string]any
	_ = json.Unmarshal(raw, &rec)
	st := toSlice(rec["steps"])
	for _, s := range st {
		sm, _ := s.(map[string]any)
		ings += len(toSlice(sm["ingredients"]))
	}
	return ings, len(st)
}

// junkFoods are pure measurement units that are never a real food (so a row whose food name is
// one of these is parser garbage). Count/quantity units that double as foods (clove, can, stick,
// head, ear, rib, leaf, slice) are deliberately excluded.
var junkFoods = map[string]bool{
	"": true, "cup": true, "cups": true, "teaspoon": true, "teaspoons": true, "tsp": true,
	"tablespoon": true, "tablespoons": true, "tbsp": true, "ounce": true, "ounces": true, "oz": true,
	"pound": true, "pounds": true, "lb": true, "lbs": true, "gram": true, "grams": true, "g": true,
	"kg": true, "ml": true, "milliliter": true, "milliliters": true, "liter": true, "liters": true,
	"l": true, "quart": true, "quarts": true, "pint": true, "pints": true, "gallon": true, "gallons": true,
	"pinch": true, "dash": true,
}

func isJunkFood(name string) bool {
	return junkFoods[strings.ToLower(strings.TrimSpace(name))]
}

func foodNameOf(p map[string]any) string {
	if fm, ok := p["food"].(map[string]any); ok {
		n, _ := fm["name"].(string)
		return n
	}
	return ""
}

// ---- helpers ----

func toSlice(v any) []any { s, _ := v.([]any); return s }
func strOr(v any) string  { s, _ := v.(string); return s }
func boolOr(v any) bool   { b, _ := v.(bool); return b }
func numOr(v any) any {
	if v == nil {
		return 0
	}
	return v
}
func tag(apply bool) string {
	if apply {
		return ""
	}
	return "  [dry run]"
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

func headWord(food string) string {
	fields := strings.Fields(strings.ToLower(food))
	if len(fields) == 0 {
		return ""
	}
	w := strings.Trim(fields[len(fields)-1], ",.;:()")
	if len(w) < 3 {
		return ""
	}
	return w
}

func mentions(instruction, word string) bool {
	if word == "" {
		return false
	}
	ins := strings.ToLower(instruction)
	for idx := 0; idx < len(ins); {
		i := strings.Index(ins[idx:], word)
		if i < 0 {
			return false
		}
		i += idx
		before := i == 0 || !isWord(ins[i-1])
		end := i + len(word)
		after := end >= len(ins) || !isWord(ins[end])
		if before && after {
			return true
		}
		idx = i + 1
	}
	return false
}

func isWord(b byte) bool {
	return b == '_' || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}
