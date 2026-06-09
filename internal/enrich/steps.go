package enrich

import (
	"strings"

	"recipearr/internal/tandoor"
)

// allocateIngredients redistributes ingredients (which scrapers usually dump in one block) to
// the first instruction step that mentions the food. Unmatched ingredients stay as a leading
// list on the first step. Heuristic by nature — toggleable per import.
func allocateIngredients(steps []tandoor.StepCreate) []tandoor.StepCreate {
	if len(steps) == 0 {
		return steps
	}

	// Pull every ingredient into a pool and clear the per-step lists.
	var pool []tandoor.IngredientCreate
	for i := range steps {
		pool = append(pool, steps[i].Ingredients...)
		steps[i].Ingredients = nil
	}
	if len(pool) == 0 {
		return steps
	}

	assigned := make([][]tandoor.IngredientCreate, len(steps))
	var unmatched []tandoor.IngredientCreate

	for _, ing := range pool {
		head := headWord(ing.Food.Name)
		placed := false
		if head != "" {
			for i := range steps {
				if mentions(steps[i].Instruction, head) {
					assigned[i] = append(assigned[i], ing)
					placed = true
					break
				}
			}
		}
		if !placed {
			unmatched = append(unmatched, ing)
		}
	}

	// Unmatched ingredients lead the first step so nothing is lost.
	assigned[0] = append(unmatched, assigned[0]...)
	for i := range steps {
		steps[i].Ingredients = assigned[i]
		steps[i].ShowIngredientsTable = true
	}
	return steps
}

// headWord returns the last meaningful word of a food name (its head noun), lowercased.
func headWord(food string) string {
	fields := strings.Fields(strings.ToLower(food))
	if len(fields) == 0 {
		return ""
	}
	w := strings.Trim(fields[len(fields)-1], ",.;:()")
	if len(w) < 3 { // too short to match reliably
		return ""
	}
	return w
}

// mentions reports whether instruction contains word as a whole-ish token (case-insensitive).
func mentions(instruction, word string) bool {
	if word == "" {
		return false
	}
	ins := strings.ToLower(instruction)
	idx := 0
	for {
		i := strings.Index(ins[idx:], word)
		if i < 0 {
			return false
		}
		i += idx
		before := i == 0 || !isWordChar(rune(ins[i-1]))
		end := i + len(word)
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
