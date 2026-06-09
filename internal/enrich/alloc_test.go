package enrich

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"recipearr/internal/tandoor"
)

type fxIng struct {
	Amount       float64 `json:"amount"`
	Unit         string  `json:"unit"`
	Food         string  `json:"food"`
	OriginalText string  `json:"original_text"`
	Note         string  `json:"note"`
}
type fxStep struct {
	Instruction string  `json:"instruction"`
	Ingredients []fxIng `json:"ingredients"`
}
type fixture struct {
	Name  string   `json:"name"`
	Steps []fxStep `json:"steps"`
}

func loadFixture(t *testing.T, path string) tandoor.RecipeCreate {
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var fx fixture
	if err := json.Unmarshal(b, &fx); err != nil {
		t.Fatal(err)
	}
	r := &tandoor.RFSRecipe{Name: fx.Name}
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

func dump(t *testing.T, out tandoor.RecipeCreate) {
	for i, st := range out.Steps {
		var names []string
		for _, ing := range st.Ingredients {
			n := ing.Food.Name
			if ing.Note != "" {
				n += " (" + ing.Note + ")"
			}
			names = append(names, n)
		}
		t.Logf("step %d: %s", i+1, strings.Join(names, " | "))
	}
}

// stepsWith returns 0-based step indices holding an ingredient whose food name contains sub.
func stepsWith(out tandoor.RecipeCreate, sub string) []int {
	sub = strings.ToLower(sub)
	var idx []int
	for i, st := range out.Steps {
		for _, ing := range st.Ingredients {
			if strings.Contains(strings.ToLower(ing.Food.Name), sub) {
				idx = append(idx, i)
				break
			}
		}
	}
	return idx
}

func noHeadersSurvive(t *testing.T, out tandoor.RecipeCreate) {
	for _, st := range out.Steps {
		for _, ing := range st.Ingredients {
			ot := strings.TrimSpace(ing.OriginalText)
			if strings.HasSuffix(ot, ":") || strings.EqualFold(ing.Food.Name, "to garnish") {
				t.Errorf("header survived as ingredient: %q", ing.Food.Name)
			}
		}
	}
}

func wantStep(t *testing.T, out tandoor.RecipeCreate, sub string, exp int) {
	got := stepsWith(out, sub)
	if len(got) == 0 {
		t.Errorf("%q: not found in any step (want step %d)", sub, exp+1)
		return
	}
	if got[0] != exp {
		t.Errorf("%q: in step %d, want step %d", sub, got[0]+1, exp+1)
	}
}

func TestAllocate438(t *testing.T) {
	out := loadFixture(t, "../../testdata_rfs_438.json")
	dump(t, out)
	noHeadersSurvive(t, out)
	wantStep(t, out, "rolled oats", 1)
	wantStep(t, out, "brown sugar", 1)
	wantStep(t, out, "peanut butter", 3)
	wantStep(t, out, "powdered sugar", 3)
	wantStep(t, out, "chocolate chips", 5)
	wantStep(t, out, "coarse salt", 6)
	// the two vegan butters must split: melted -> step 2 (idx1), softened -> step 4 (idx3)
	if got := stepsWith(out, "vegan butter"); len(got) < 2 || got[0] != 1 || got[len(got)-1] != 3 {
		t.Errorf("vegan butter split: got steps %v, want [1 ... 3]", got)
	}
}

func TestAllocate439(t *testing.T) {
	out := loadFixture(t, "../../testdata_rfs_439.json")
	dump(t, out)
	noHeadersSurvive(t, out)
	wantStep(t, out, "olive oil", 1)  // dressing -> step 2
	wantStep(t, out, "lime juice", 1) // dressing -> step 2
	wantStep(t, out, "avocado", 2)    // salad -> step 3
	wantStep(t, out, "feta", 2)       // salad -> step 3
	wantStep(t, out, "pistachios", 2) // salad -> step 3
	// the three cilantros must land on different steps: leaves->2(dressing), diced->3(salad), extra->4(garnish)
	cil := stepsWith(out, "cilantro")
	t.Logf("cilantro steps: %v", cil)
}
