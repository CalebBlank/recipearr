package enrich

import (
	"sort"
	"strings"

	"recipearr/internal/tandoor"
)

// Ing is the minimal ingredient view the allocator needs. It's the shared input for both the
// import path (built from the parsed pool) and the library reprocessor (built from stored recipe
// JSON), so allocation logic lives in exactly one place.
type Ing struct {
	Food         string
	OriginalText string
	Note         string
	IsHeader     bool // already flagged a header (trusted); otherwise header detection applies
}

// AssignSteps decides which instruction step each ingredient in a flat, original-order pool belongs
// to. Returns stepOf[i] = the step index for pool[i], or -1 if pool[i] is a section header (the
// caller decides whether to drop it or keep it as a divider). Component-aware: section headers
// ("For the salad:", "To garnish") group the ingredients that follow, ambiguous members are biased
// to their component's cluster step, and same-named duplicates are split by their modifier words.
func AssignSteps(pool []Ing, instructions []string) []int {
	stepOf := make([]int, len(pool))
	if len(pool) == 0 {
		return stepOf
	}
	if len(instructions) == 0 {
		for i := range pool {
			if isHeaderIng(pool[i]) {
				stepOf[i] = -1
			}
		}
		return stepOf
	}

	lowerSteps := make([]string, len(instructions))
	for i := range instructions {
		lowerSteps[i] = strings.ToLower(instructions[i])
	}

	// Parse components: mark headers (-1) and group the ingredients that follow each one.
	var keptIdx []int  // pool indices that are real ingredients
	var groups [][]int // each group is a list of positions into keptIdx
	cur := -1
	ensure := func() {
		if cur < 0 {
			groups = append(groups, nil)
			cur = len(groups) - 1
		}
	}
	for pi := range pool {
		if isHeaderIng(pool[pi]) {
			stepOf[pi] = -1
			groups = append(groups, nil)
			cur = len(groups) - 1
			continue
		}
		ensure()
		groups[cur] = append(groups[cur], len(keptIdx))
		keptIdx = append(keptIdx, pi)
	}
	if len(keptIdx) == 0 {
		return stepOf
	}

	candidates := make([][]int, len(keptIdx))
	for k, pi := range keptIdx {
		candidates[k] = matchSteps(strings.ToLower(pool[pi].Food), lowerSteps)
	}

	placed := make([]int, len(keptIdx))
	for i := range placed {
		placed[i] = -1
	}
	for _, g := range groups {
		home := componentHome(g, candidates)
		for _, ki := range g {
			switch cand := candidates[ki]; {
			case len(cand) == 1:
				placed[ki] = cand[0]
			case len(cand) > 1:
				placed[ki] = pickNearHome(cand, home)
			default:
				placed[ki] = home
			}
		}
		spreadDuplicates(g, keptIdx, pool, lowerSteps, placed)
	}

	for k, pi := range keptIdx {
		if s := placed[k]; s >= 0 && s < len(instructions) {
			stepOf[pi] = s
		} else {
			stepOf[pi] = 0 // unmatched -> first step
		}
	}
	return stepOf
}

// allocateIngredients (import path) redistributes pooled ingredients onto the steps that use them,
// dropping section-header pseudo-ingredients. Thin wrapper over AssignSteps.
func allocateIngredients(steps []tandoor.StepCreate) []tandoor.StepCreate {
	if len(steps) == 0 {
		return steps
	}
	var pool []tandoor.IngredientCreate
	for i := range steps {
		pool = append(pool, steps[i].Ingredients...)
		steps[i].Ingredients = nil
	}
	if len(pool) == 0 {
		return steps
	}

	ings := make([]Ing, len(pool))
	instr := make([]string, len(steps))
	for i := range steps {
		instr[i] = steps[i].Instruction
	}
	for i, ic := range pool {
		hdr := ic.Amount == 0 && ic.Unit == nil && DetectHeader(ic.Food.Name, ic.OriginalText)
		ings[i] = Ing{Food: ic.Food.Name, OriginalText: ic.OriginalText, Note: ic.Note, IsHeader: hdr}
	}
	stepOf := AssignSteps(ings, instr)

	assigned := make([][]tandoor.IngredientCreate, len(steps))
	for i, ic := range pool {
		if s := stepOf[i]; s >= 0 { // s == -1 means header -> dropped on import
			assigned[s] = append(assigned[s], ic)
		}
	}
	for i := range steps {
		steps[i].Ingredients = assigned[i]
		steps[i].ShowIngredientsTable = true
	}
	return steps
}

// isHeaderIng trusts the caller's IsHeader flag — the caller (import or reprocess) owns header
// detection because it has the amount/unit context (and any stored is_header field) that AssignSteps
// deliberately doesn't.
func isHeaderIng(ing Ing) bool { return ing.IsHeader }

// DetectHeader reports whether a row reads like a section header rather than a food: its original
// text ends with ":" or starts with "for the …" / "to garnish/serve". Callers AND it with the
// amount/unit gate (a header has no amount and no unit) and any stored is_header flag.
func DetectHeader(food, originalText string) bool {
	ot := strings.TrimSpace(originalText)
	if strings.HasSuffix(ot, ":") {
		return true
	}
	probe := strings.ToLower(ot)
	if probe == "" {
		probe = strings.ToLower(strings.TrimSpace(food))
	}
	for _, p := range []string{"for the ", "for serving", "to garnish", "to serve", "to assemble", "to finish", "to top"} {
		if strings.HasPrefix(probe, p) {
			return true
		}
	}
	return probe == "garnish" || probe == "topping" || probe == "toppings"
}

// matchSteps returns the step indices (ascending) that mention the food by its LONGEST matching
// phrase: full name, then contiguous sub-phrases down to single words. So "creamy peanut butter"
// matches on "peanut butter" (not bare "butter"), and "feta cheese" on "feta".
func matchSteps(food string, lowerSteps []string) []int {
	words := strings.Fields(food)
	for n := len(words); n >= 1; n-- {
		var hits []int
		for start := 0; start+n <= len(words); start++ {
			phrase := strings.Join(words[start:start+n], " ")
			if n == 1 && len(phrase) < 3 {
				continue
			}
			for si, ins := range lowerSteps {
				if mentions(ins, phrase) {
					hits = appendUnique(hits, si)
				}
			}
		}
		if len(hits) > 0 {
			sort.Ints(hits)
			return hits
		}
	}
	return nil
}

// componentHome returns the most common step among a component's UNAMBIGUOUS (single-candidate)
// ingredients — the step its members cluster on — or -1 if none are unambiguous.
func componentHome(group []int, candidates [][]int) int {
	count := map[int]int{}
	for _, ki := range group {
		if len(candidates[ki]) == 1 {
			count[candidates[ki][0]]++
		}
	}
	best, bestN := -1, 0
	for s, n := range count {
		if n > bestN || (n == bestN && (best < 0 || s < best)) {
			best, bestN = s, n
		}
	}
	return best
}

func pickNearHome(cand []int, home int) int {
	if home < 0 {
		return cand[0]
	}
	best, bestDist := cand[0], 1<<30
	for _, s := range cand {
		if s == home {
			return home
		}
		d := s - home
		if d < 0 {
			d = -d
		}
		if d < bestDist {
			best, bestDist = s, d
		}
	}
	return best
}

// spreadDuplicates separates N identical-named ingredients in a component. group holds positions
// into keptIdx; keptIdx[pos] indexes pool.
func spreadDuplicates(group []int, keptIdx []int, pool []Ing, lowerSteps []string, placed []int) {
	byFood := map[string][]int{}
	var order []string
	for _, ki := range group {
		f := strings.ToLower(strings.TrimSpace(pool[keptIdx[ki]].Food))
		if _, ok := byFood[f]; !ok {
			order = append(order, f)
		}
		byFood[f] = append(byFood[f], ki)
	}
	for _, f := range order {
		dupes := byFood[f]
		k := len(dupes)
		if k < 2 {
			continue
		}

		// (1) Modifier routing: prep words unique to each copy (from original_text, e.g. "melted"
		// vs "softened") name the step that uses it. Score: modifier words 2x, food words 1x.
		foodWords := strings.Fields(f)
		tentative := make([]int, k)
		routed, anyMod := true, false
		used := map[int]bool{}
		for j, ki := range dupes {
			mods := modifierWords(pool[keptIdx[ki]].OriginalText, f)
			if len(mods) > 0 {
				anyMod = true
			}
			best, bestScore := -1, 0
			for si, ins := range lowerSteps {
				score := 0
				for _, w := range mods {
					if mentions(ins, w) {
						score += 2
					}
				}
				for _, w := range foodWords {
					if len(w) >= 3 && mentions(ins, w) {
						score++
					}
				}
				if score > bestScore {
					best, bestScore = si, score
				}
			}
			if best < 0 || used[best] {
				routed = false
				break
			}
			used[best] = true
			tentative[j] = best
		}
		if routed && anyMod {
			for j, ki := range dupes {
				placed[ki] = tentative[j]
			}
			continue
		}

		// (2) Bounded spread across a broadened phrase's steps (size close to k, so common staples
		// mentioned in many steps are left piled rather than scattered).
		words := strings.Fields(f)
		var set []int
		for n := len(words); n >= 1 && set == nil; n-- {
			for start := 0; start+n <= len(words); start++ {
				phrase := strings.Join(words[start:start+n], " ")
				if n == 1 && len(phrase) < 3 {
					continue
				}
				var hits []int
				for si, ins := range lowerSteps {
					if mentions(ins, phrase) {
						hits = appendUnique(hits, si)
					}
				}
				if len(hits) >= k && len(hits) <= k+1 {
					sort.Ints(hits)
					set = hits
					break
				}
			}
		}
		if set != nil {
			for j, ki := range dupes {
				placed[ki] = set[min(j, len(set)-1)]
			}
		}
	}
}

// modifierWords returns distinguishing words from an ingredient's original text that aren't part of
// its cleaned food name — e.g. "melted" from "1/2 cup melted vegan butter" (food "vegan butter").
func modifierWords(originalText, foodName string) []string {
	inFood := map[string]bool{}
	for _, w := range strings.Fields(strings.ToLower(foodName)) {
		inFood[w] = true
	}
	var out []string
	for _, w := range strings.Fields(strings.ToLower(originalText)) {
		w = strings.Trim(w, ",.;:()\"")
		if len(w) < 4 || inFood[w] {
			continue
		}
		alpha := true
		for _, r := range w {
			if r < 'a' || r > 'z' {
				alpha = false
				break
			}
		}
		if !alpha {
			continue
		}
		dup := false
		for _, x := range out {
			if x == w {
				dup = true
				break
			}
		}
		if !dup {
			out = append(out, w)
		}
	}
	return out
}

func appendUnique(s []int, v int) []int {
	for _, x := range s {
		if x == v {
			return s
		}
	}
	return append(s, v)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// mentions reports whether instruction contains phrase as a whole-ish token run (case-insensitive).
func mentions(instruction, phrase string) bool {
	if phrase == "" {
		return false
	}
	ins := strings.ToLower(instruction)
	idx := 0
	for {
		i := strings.Index(ins[idx:], phrase)
		if i < 0 {
			return false
		}
		i += idx
		before := i == 0 || !isWordChar(rune(ins[i-1]))
		end := i + len(phrase)
		after := end >= len(ins) || !isWordChar(rune(ins[end]))
		if before && after {
			return true
		}
		idx = i + 1
		if idx >= len(ins) {
			return false
		}
	}
}

func isWordChar(r rune) bool {
	return r == '_' ||
		(r >= 'a' && r <= 'z') ||
		(r >= 'A' && r <= 'Z') ||
		(r >= '0' && r <= '9')
}
