package enrich

import (
	"sort"
	"strings"

	"recipearr/internal/tandoor"
)

// allocateIngredients redistributes pooled ingredients (scrapers dump them all in one block) onto
// the instruction steps that use them. It is component-aware: ingredient-list section headers
// ("For the salad:", "Cilantro Lime Dressing:", "To garnish") are detected, dropped, and used to
// group the ingredients that follow — so ambiguous members (a second "cilantro", a "vegan butter"
// used twice) are biased toward where the rest of their component lands. Heuristic; toggleable.
func allocateIngredients(steps []tandoor.StepCreate) []tandoor.StepCreate {
	if len(steps) == 0 {
		return steps
	}

	// Pull every ingredient into a single ordered pool; clear the per-step lists.
	var pool []tandoor.IngredientCreate
	for i := range steps {
		pool = append(pool, steps[i].Ingredients...)
		steps[i].Ingredients = nil
	}
	if len(pool) == 0 {
		return steps
	}

	lowerSteps := make([]string, len(steps))
	for i := range steps {
		lowerSteps[i] = strings.ToLower(steps[i].Instruction)
	}

	// --- parse components: drop header lines, group the ingredients that follow each header ---
	var kept []tandoor.IngredientCreate
	var groups [][]int // each is a list of indices into kept (a component, or the leading "" group)
	cur := -1
	ensure := func() {
		if cur < 0 {
			groups = append(groups, nil)
			cur = len(groups) - 1
		}
	}
	for _, ing := range pool {
		if isHeaderIngredient(ing) {
			groups = append(groups, nil)
			cur = len(groups) - 1
			continue // drop the header pseudo-ingredient itself
		}
		ensure()
		kept = append(kept, ing)
		groups[cur] = append(groups[cur], len(kept)-1)
	}
	if len(kept) == 0 {
		return steps
	}

	// --- per-ingredient candidate steps, by the longest matching phrase ---
	candidates := make([][]int, len(kept))
	for i := range kept {
		candidates[i] = matchSteps(strings.ToLower(kept[i].Food.Name), lowerSteps)
	}

	placed := make([]int, len(kept))
	for i := range placed {
		placed[i] = -1
	}

	for _, g := range groups {
		home := componentHome(g, candidates) // mode of unambiguous matches in this component; -1 if none
		for _, ki := range g {
			switch cand := candidates[ki]; {
			case len(cand) == 1:
				placed[ki] = cand[0]
			case len(cand) > 1:
				placed[ki] = pickNearHome(cand, home)
			default:
				placed[ki] = home // unmatched -> component's home step (may be -1)
			}
		}
		spreadDuplicates(g, kept, lowerSteps, placed) // two "vegan butter" -> different butter steps
	}

	// --- write back; anything still unplaced leads the first step so nothing is lost ---
	assigned := make([][]tandoor.IngredientCreate, len(steps))
	var unmatched []tandoor.IngredientCreate
	for ki := range kept {
		if s := placed[ki]; s >= 0 && s < len(steps) {
			assigned[s] = append(assigned[s], kept[ki])
		} else {
			unmatched = append(unmatched, kept[ki])
		}
	}
	assigned[0] = append(unmatched, assigned[0]...)
	for i := range steps {
		steps[i].Ingredients = assigned[i]
		steps[i].ShowIngredientsTable = true
	}
	return steps
}

// isHeaderIngredient reports whether a pooled ingredient is really a section header ("For the
// salad:", "To garnish") rather than a food. Headers have no amount/unit and either end their
// original text with a colon or read like a section label ("for …", "to garnish/serve").
func isHeaderIngredient(ing tandoor.IngredientCreate) bool {
	if ing.Amount != 0 || ing.Unit != nil {
		return false
	}
	ot := strings.TrimSpace(ing.OriginalText)
	if strings.HasSuffix(ot, ":") {
		return true
	}
	probe := strings.ToLower(ot)
	if probe == "" {
		probe = strings.ToLower(strings.TrimSpace(ing.Food.Name))
	}
	for _, p := range []string{"for the ", "for serving", "to garnish", "to serve", "to assemble", "to finish", "to top"} {
		if strings.HasPrefix(probe, p) {
			return true
		}
	}
	return probe == "garnish" || probe == "topping" || probe == "toppings"
}

// matchSteps returns the step indices (ascending) that mention the food by its LONGEST matching
// phrase: it tries the full name, then every contiguous sub-phrase down to single words, and
// returns the matches for the first (longest) phrase length that hits any step. So "creamy peanut
// butter" matches on "peanut butter" (not bare "butter"), and "feta cheese" on "feta".
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

// componentHome returns the most common step among the component's UNAMBIGUOUS (single-candidate)
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

// pickNearHome chooses, among an ingredient's candidate steps, the component home if it's a
// candidate, else the candidate nearest the home, else the first candidate.
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

// spreadDuplicates handles N identical-named ingredients in a component (e.g. melted + softened
// "vegan butter") by distributing them across a broadened phrase's steps — but ONLY when that
// broadened set is close to N (size N or N+1), so common staples (salt mentioned in 4 steps) are
// left piled rather than scattered.
func spreadDuplicates(group []int, kept []tandoor.IngredientCreate, lowerSteps []string, placed []int) {
	byFood := map[string][]int{}
	var order []string
	for _, ki := range group {
		f := strings.ToLower(strings.TrimSpace(kept[ki].Food.Name))
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
		// vs "softened") often name the step that uses it. If every copy's modifiers pin a DISTINCT
		// single step, route by that — this separates two "vegan butter"s even when bare "butter"
		// is too common to spread safely.
		foodWords := strings.Fields(f)
		tentative := make([]int, k)
		routed := true
		anyMod := false
		used := map[int]bool{}
		for j, ki := range dupes {
			mods := modifierWords(kept[ki].OriginalText, f)
			if len(mods) > 0 {
				anyMod = true
			}
			best, bestScore := -1, 0
			for si, ins := range lowerSteps {
				score := 0
				for _, w := range mods { // modifier (prep) words weigh double — they distinguish copies
					if mentions(ins, w) {
						score += 2
					}
				}
				for _, w := range foodWords {
					if len(w) >= 3 && mentions(ins, w) {
						score++
					}
				}
				if score > bestScore { // first (earliest) step wins ties
					best, bestScore = si, score
				}
			}
			if best < 0 || used[best] { // not distinguishable -> bail to bounded spread
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

		// (2) Bounded spread across a broadened phrase's steps (size close to k).
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
				if len(hits) >= k && len(hits) <= k+1 { // close to k -> safe to spread
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
// These prep words often name the step that uses the ingredient.
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
