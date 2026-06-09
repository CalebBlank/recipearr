package enrich

import (
	"strings"
	"testing"

	"recipearr/internal/tandoor"
)

func TestCleanFoodName(t *testing.T) {
	cases := []struct {
		in       string
		wantName string
		wantNote string // substring expected in note ("" = note ignored)
	}{
		{"(1.75 ounces; 50g) sugar", "sugar", "1.75 ounces; 50g"},
		{"sugar or brown sugar", "sugar", "or brown sugar"},
		{"finely chopped white onion", "white onion", ""},
		{"50g sugar", "sugar", ""},
		{"ground coriander", "ground coriander", ""}, // meaning preserved
		{"red onion", "red onion", ""},                // variety preserved
		{"lime juice (from about 1 limes)", "lime juice", "from about 1 limes"},
		{"  kosher salt  ", "kosher salt", ""},
		{"pork belly, cut into thin strips", "pork belly", "cut into thin strips"},
		{"jalapeño, seeds and ribs removed, finely chopped", "jalapeño", "seeds and ribs removed"},
		{"15-ounce cans chickpeas", "chickpeas", ""},     // stacked units stripped
		{"15 ounce can crushed tomatoes", "tomatoes", ""}, // unit + unit + prep
	}
	for _, c := range cases {
		gotName, gotNote := cleanFoodName(c.in)
		if gotName != c.wantName {
			t.Errorf("cleanFoodName(%q) name = %q, want %q", c.in, gotName, c.wantName)
		}
		if c.wantNote != "" && !strings.Contains(gotNote, c.wantNote) {
			t.Errorf("cleanFoodName(%q) note = %q, want to contain %q", c.in, gotNote, c.wantNote)
		}
	}
}

func TestCleanDescription(t *testing.T) {
	in := "<p>Celebrate &amp; enjoy this dish.</p> Jump to Recipe"
	got := cleanDescription(in, "clean")
	if got != "Celebrate & enjoy this dish." {
		t.Errorf("cleanDescription clean = %q", got)
	}
	if got := cleanDescription(in, "blank"); got != "" {
		t.Errorf("cleanDescription blank = %q, want empty", got)
	}
	if got := cleanDescription("  raw text  ", "raw"); got != "raw text" {
		t.Errorf("cleanDescription raw = %q", got)
	}
}

func TestDetectDietTags(t *testing.T) {
	got := detectDietTags("this vegan 30-minute weeknight pasta", defaultDietTaxonomy())
	want := map[string]bool{"vegan": true, "quick": true}
	if len(got) != len(want) {
		t.Fatalf("detectDietTags = %v, want vegan+quick", got)
	}
	for _, g := range got {
		if !want[g] {
			t.Errorf("unexpected tag %q in %v", g, got)
		}
	}
}

func TestCurateTagsViaTransform(t *testing.T) {
	r := &tandoor.RFSRecipe{
		Name:        "Best Guacamole",
		Description: "An easy vegan dip.",
		Keywords: []tandoor.RFSKeyword{
			{Name: "appetizer", ImportKeyword: true},
			{Name: "mexican", ImportKeyword: true},
			{Name: "cookieandkate.com", ImportKeyword: false}, // noise: domain
			{Name: "kate", ImportKeyword: false},              // noise: author
		},
		Steps: []tandoor.RFSStep{{Instruction: "Mash."}},
	}
	out := Transform(r, DefaultOptions())

	have := map[string]bool{}
	for _, k := range out.Keywords {
		have[strings.ToLower(k.Name)] = true
	}
	for _, want := range []string{"appetizer", "mexican", "vegan"} {
		if !have[want] {
			t.Errorf("expected tag %q, got %v", want, out.Keywords)
		}
	}
	if have["recipearr"] {
		t.Error("the recipearr tag should no longer be added")
	}
	for _, noise := range []string{"cookieandkate.com", "kate"} {
		if have[noise] {
			t.Errorf("noise tag %q should have been dropped", noise)
		}
	}
}

func TestUnitFolding(t *testing.T) {
	r := &tandoor.RFSRecipe{Name: "t", Steps: []tandoor.RFSStep{{Instruction: "combine everything", Ingredients: []tandoor.RFSIngredient{
		{Amount: 4, Unit: &tandoor.RFSUnit{Name: "guajillo"}, Food: &tandoor.RFSFood{Name: "chiles"}},
		{Amount: 3, Unit: &tandoor.RFSUnit{Name: "árbol"}, Food: &tandoor.RFSFood{Name: "chiles"}},
		{Amount: 1, Unit: &tandoor.RFSUnit{Name: "chipotle"}, Food: &tandoor.RFSFood{Name: "chile"}},
		{Amount: 4, Unit: &tandoor.RFSUnit{Name: "flour"}, Food: &tandoor.RFSFood{Name: "tortillas"}},
		{Amount: 3, Unit: &tandoor.RFSUnit{Name: "cloves"}, Food: &tandoor.RFSFood{Name: "garlic"}},
		{Amount: 8, Unit: &tandoor.RFSUnit{Name: "ounces"}, Food: &tandoor.RFSFood{Name: "queso Oaxaca"}},
	}}}}

	out := Transform(r, DefaultOptions())
	got := map[string]string{} // food -> unit
	for _, st := range out.Steps {
		for _, ing := range st.Ingredients {
			u := ""
			if ing.Unit != nil {
				u = ing.Unit.Name
			}
			got[ing.Food.Name] = u
		}
	}
	want := []struct{ food, unit string }{
		{"guajillo chiles", ""}, // bogus unit folded into food
		{"árbol chiles", ""},
		{"chipotle chile", ""},
		{"flour tortillas", ""},
		{"garlic", "cloves"},      // real unit kept
		{"queso Oaxaca", "ounces"}, // real unit kept
	}
	for _, c := range want {
		u, ok := got[c.food]
		if !ok {
			t.Errorf("expected food %q in output; got %v", c.food, got)
			continue
		}
		if u != c.unit {
			t.Errorf("food %q: unit = %q, want %q", c.food, u, c.unit)
		}
	}
}

func TestAllocateIngredients(t *testing.T) {
	steps := []tandoor.StepCreate{
		{Instruction: "Mix the flour and sugar.", Ingredients: []tandoor.IngredientCreate{
			{Food: tandoor.FoodRef{Name: "flour"}},
			{Food: tandoor.FoodRef{Name: "sugar"}},
			{Food: tandoor.FoodRef{Name: "eggs"}},
		}},
		{Instruction: "Beat the eggs, then fold in.", Ingredients: nil},
	}
	got := allocateIngredients(steps)
	if len(got[1].Ingredients) != 1 || got[1].Ingredients[0].Food.Name != "eggs" {
		t.Errorf("expected eggs allocated to step 2, got %+v", got[1].Ingredients)
	}
	if len(got[0].Ingredients) != 2 {
		t.Errorf("expected flour+sugar in step 1, got %+v", got[0].Ingredients)
	}
}
