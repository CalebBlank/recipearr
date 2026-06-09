package enrich

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"recipearr/internal/tandoor"
)

// loadLive loads a captured fixture INCLUDING the top-level description (the import path uses
// r.Description), so we can see exactly what the allocator does on real re-fetched recipes.
func loadLive(t *testing.T, path string) tandoor.RecipeCreate {
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var fx struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Steps       []struct {
			Instruction string `json:"instruction"`
			Ingredients []struct {
				Amount       float64 `json:"amount"`
				Unit         string  `json:"unit"`
				Food         string  `json:"food"`
				OriginalText string  `json:"original_text"`
				Note         string  `json:"note"`
			} `json:"ingredients"`
		} `json:"steps"`
	}
	if err := json.Unmarshal(b, &fx); err != nil {
		t.Fatal(err)
	}
	r := &tandoor.RFSRecipe{Name: fx.Name, Description: fx.Description}
	for _, s := range fx.Steps {
		st := tandoor.RFSStep{Instruction: s.Instruction}
		for _, ing := range s.Ingredients {
			ri := tandoor.RFSIngredient{Amount: ing.Amount, Note: ing.Note, OriginalText: ing.OriginalText}
			if ing.Food != "" {
				ri.Food = &tandoor.RFSFood{Name: ing.Food}
			}
			if ing.Unit != "" {
				ri.Unit = &tandoor.RFSUnit{Name: ing.Unit}
			}
			st.Ingredients = append(st.Ingredients, ri)
		}
		r.Steps = append(r.Steps, st)
	}
	return Transform(r, DefaultOptions())
}

func TestDumpLive(t *testing.T) {
	for _, slug := range []string{"av_live", "veg_hotchoc", "veg_wontons", "veg_poutine"} {
		out := loadLive(t, "../../testdata_rfs_"+slug+".json")
		t.Logf("######## %s — %q", slug, out.Name)
		t.Logf("  description: %q", truncate(out.Description, 80))
		for i, st := range out.Steps {
			var names []string
			for _, ing := range st.Ingredients {
				names = append(names, ing.Food.Name)
			}
			ins := truncate(strings.ReplaceAll(st.Instruction, "\n", " "), 60)
			t.Logf("  step %d [%s]: %s", i+1, ins, strings.Join(names, " | "))
		}
	}
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n {
		return s[:n] + "..."
	}
	return s
}
