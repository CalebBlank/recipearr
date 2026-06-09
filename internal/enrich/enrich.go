// Package enrich is the import-quality layer between recipe-from-source and the Tandoor
// create call. It fixes the rough edges of Tandoor's native importer:
//   - communizing foods (clean leaked weights/parentheticals, resolve "X or Y", reuse foods)
//   - curating tags (keep meaningful ones, drop domain/author noise, add diet/style tags)
//   - cleaning descriptions (strip HTML/entities/boilerplate)
//   - allocating ingredients to the step that uses them
//
// Each enricher is individually toggleable. The transform is the single place that maps a
// parsed recipe (tandoor.RFSRecipe) to a create payload (tandoor.RecipeCreate).
package enrich

import (
	"strings"

	"recipearr/internal/tandoor"
)

type Options struct {
	Communize       bool
	CleanDescription bool
	CurateTags      bool
	AllocateSteps   bool

	DescriptionMode string              // "clean" | "raw" | "blank"
	Aliases         map[string]string   // lowercased food alias -> canonical name
	DietTaxonomy    map[string][]string // tag -> trigger phrases (lowercased)
	TagDenylist     []string            // lowercased substrings; tags containing one are dropped
	ExtraKeywords   []string            // appended to every recipe (source defaults + "recipearr")
}

// DefaultOptions enables all enrichers with the built-in taxonomy and denylist.
func DefaultOptions() Options {
	return Options{
		Communize:        true,
		CleanDescription: true,
		CurateTags:       true,
		AllocateSteps:    true,
		DescriptionMode:  "clean",
		Aliases:          map[string]string{},
		DietTaxonomy:     defaultDietTaxonomy(),
		TagDenylist:      defaultTagDenylist(),
		ExtraKeywords:    nil,
	}
}

// Transform converts a parsed recipe into a Tandoor create payload, applying enabled enrichers.
func Transform(r *tandoor.RFSRecipe, opts Options) tandoor.RecipeCreate {
	out := tandoor.RecipeCreate{
		Name:         strings.TrimSpace(r.Name),
		Internal:     true,
		WorkingTime:  r.WorkingTime,
		WaitingTime:  r.WaitingTime,
		Servings:     r.Servings,
		ServingsText: strings.TrimSpace(r.ServingsText),
		SourceURL:    r.SourceURL,
	}

	desc := r.Description
	if opts.CleanDescription {
		desc = cleanDescription(desc, opts.DescriptionMode)
	}
	out.Description = desc

	steps := convertSteps(r.Steps, opts)
	if opts.AllocateSteps {
		steps = allocateIngredients(steps)
	}
	if len(steps) == 0 {
		steps = []tandoor.StepCreate{{ShowIngredientsTable: true}}
	}
	// Tandoor iterates step.ingredients server-side; a nil slice marshals to JSON null
	// ('NoneType' object is not iterable). Guarantee empty-but-present lists.
	for i := range steps {
		if steps[i].Ingredients == nil {
			steps[i].Ingredients = []tandoor.IngredientCreate{}
		}
	}
	out.Steps = steps

	out.Keywords = curateTags(r, opts)
	if out.Keywords == nil {
		out.Keywords = []tandoor.KeywordRef{}
	}
	return out
}

func convertSteps(in []tandoor.RFSStep, opts Options) []tandoor.StepCreate {
	out := make([]tandoor.StepCreate, 0, len(in))
	for i, s := range in {
		out = append(out, tandoor.StepCreate{
			Instruction:          strings.TrimSpace(s.Instruction),
			Ingredients:          convertIngredients(s.Ingredients, opts),
			ShowIngredientsTable: true,
			Order:                i,
		})
	}
	return out
}

func convertIngredients(in []tandoor.RFSIngredient, opts Options) []tandoor.IngredientCreate {
	out := make([]tandoor.IngredientCreate, 0, len(in))
	for _, ing := range in {
		foodName := ""
		if ing.Food != nil {
			foodName = ing.Food.Name
		}
		unitName := ""
		if ing.Unit != nil {
			unitName = strings.TrimSpace(ing.Unit.Name)
		}
		note := strings.TrimSpace(ing.Note)
		if opts.Communize {
			// A mis-parsed "unit" that isn't a real measurement (e.g. "guajillo", "flour",
			// "large") belongs in the food name, not the unit column.
			if unitName != "" && !isKnownUnit(unitName) {
				foodName = strings.TrimSpace(unitName + " " + foodName)
				unitName = ""
			}
			clean, extra := cleanFoodName(foodName)
			foodName = communize(clean, opts.Aliases)
			note = joinNote(note, extra)
		}
		// Lowercase food names for consistency (Tandoor reuses foods by exact name, so stable casing
		// collapses "Powdered sugar"/"powdered sugar" into one food).
		foodName = strings.ToLower(strings.TrimSpace(foodName))
		if foodName == "" {
			// Without a food name Tandoor has nothing to link; skip rather than create junk.
			continue
		}
		var unit *tandoor.UnitRef
		if unitName != "" {
			unit = &tandoor.UnitRef{Name: unitName}
		}
		out = append(out, tandoor.IngredientCreate{
			Food:         tandoor.FoodRef{Name: foodName},
			Unit:         unit,
			Amount:       ing.Amount,
			Note:         note,
			OriginalText: strings.TrimSpace(ing.OriginalText),
			NoAmount:     ing.Amount == 0,
		})
	}
	return out
}

func curateTags(r *tandoor.RFSRecipe, opts Options) []tandoor.KeywordRef {
	seen := map[string]bool{}
	var out []tandoor.KeywordRef
	add := func(name string) {
		n := strings.TrimSpace(name)
		if n == "" {
			return
		}
		key := strings.ToLower(n)
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, tandoor.KeywordRef{Name: n})
	}

	for _, k := range r.Keywords {
		name := strings.TrimSpace(k.Name)
		if name == "" {
			continue
		}
		if opts.CurateTags {
			if !k.ImportKeyword { // drop scrape noise: domain, author, raw ingredient tags
				continue
			}
			if denylisted(name, opts.TagDenylist) {
				continue
			}
		}
		add(name)
	}

	if opts.CurateTags {
		text := strings.ToLower(r.Name + " " + r.Description + " " + keywordsText(r.Keywords))
		for _, tag := range detectDietTags(text, opts.DietTaxonomy) {
			add(tag)
		}
	}

	for _, e := range opts.ExtraKeywords {
		add(e)
	}
	return out
}

func denylisted(name string, list []string) bool {
	ln := strings.ToLower(name)
	for _, d := range list {
		if d != "" && strings.Contains(ln, d) {
			return true
		}
	}
	return false
}

func keywordsText(ks []tandoor.RFSKeyword) string {
	var b strings.Builder
	for _, k := range ks {
		b.WriteString(k.Name)
		b.WriteByte(' ')
		b.WriteString(k.Label)
		b.WriteByte(' ')
	}
	return b.String()
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

// CommunizeIngredient applies the same food normalization used on import to an existing
// (food, unit) pair: folds a non-measurement "unit" (e.g. "guajillo", "flour") into the food,
// cleans leaked weights/parentheticals/prep, resolves "X or Y", and applies aliases. Returns the
// cleaned food name, the possibly-cleared unit, and any text to append to the ingredient note.
func CommunizeIngredient(food, unit string, aliases map[string]string) (newFood, newUnit, extraNote string) {
	food = strings.TrimSpace(food)
	unit = strings.TrimSpace(unit)
	if unit != "" && !isKnownUnit(unit) {
		food = strings.TrimSpace(unit + " " + food)
		unit = ""
	}
	clean, extra := cleanFoodName(food)
	// Lowercase to match the import path (collapses "Powdered sugar"/"powdered sugar" into one food).
	return strings.ToLower(strings.TrimSpace(communize(clean, aliases))), unit, extra
}

func communize(name string, aliases map[string]string) string {
	key := strings.ToLower(strings.TrimSpace(name))
	if key == "" {
		return name
	}
	if canon, ok := aliases[key]; ok {
		return canon
	}
	return name
}
