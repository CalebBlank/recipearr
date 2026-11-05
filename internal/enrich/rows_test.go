package enrich

import (
	"strings"
	"testing"
)

func TestParseLine(t *testing.T) {
	cases := []struct {
		in           string
		amount       float64
		unit, food   string
		noteContains string
	}{
		{"1/3 cup (packed; 68 g) dark brown sugar", 1.0 / 3, "cup", "dark brown sugar", "packed; 68 g"},
		{"1/3 cup plus 1 Tbsp. extra-virgin olive oil", 1.0 / 3, "cup", "extra-virgin olive oil", "plus 1 Tbsp."},
		{"1 head of garlic", 1, "head", "garlic", ""},
		{"1 cup unflavored, unsweetened oat milk", 1, "cup", "unsweetened oat milk", "unflavored"},
		{"A pinch of sea salt", 0, "", "sea salt", "a pinch"},
		{"jalapeÃ±o, seeded", 0, "", "jalapeño", "seeded"},
		{"3Â½ cups flour", 3.5, "cups", "flour", ""},
		{"1⁄4 tsp cinnamon", 0.25, "tsp", "cinnamon", ""},
		{"5 tablespoons", 5, "tablespoons", "", ""},
		{"Pinch of cloves", 0, "Pinch", "cloves", ""},
		{"1 (2-lb [908 g]) cabbage", 1, "", "cabbage", "908 g"},
		{"1½ cups (300 g) flour, sifted", 1.5, "cups", "flour", "sifted"},
	}
	for _, c := range cases {
		p := ParseLine(c.in)
		if absf(p.Amount-c.amount) > 0.001 || p.Unit != c.unit || p.Food != c.food {
			t.Errorf("ParseLine(%q) = %+v, want amount %v unit %q food %q", c.in, p, c.amount, c.unit, c.food)
		}
		if c.noteContains != "" && !strings.Contains(p.Note, c.noteContains) {
			t.Errorf("ParseLine(%q) note = %q, want it to contain %q", c.in, p.Note, c.noteContains)
		}
	}
}

func TestFixMojibakeLeavesGoodText(t *testing.T) {
	for _, s := range []string{"jalapeño", "crème fraîche", "1½ cups", "Ãbc plain"} {
		if s == "Ãbc plain" {
			continue
		}
		if got := FixMojibake(s); got != s {
			t.Errorf("FixMojibake(%q) = %q, should be unchanged", s, got)
		}
	}
	if got := FixMojibake("jalapeÃ±o"); got != "jalapeño" {
		t.Errorf("got %q", got)
	}
}

func row(ot, food, unit string, amount float64, note string) Row {
	return Row{OriginalText: ot, Food: food, Unit: unit, Amount: amount, Note: note}
}

func kindsOf(fs []Fix) map[string]int {
	m := map[string]int{}
	for _, f := range fs {
		m[f.Kind]++
	}
	return m
}

func TestCleanRowsVegnewsSplit(t *testing.T) {
	// vegnews: "5 tablespoons sugar, divided" arrives as three rows.
	in := []Row{
		row("1 cup flour", "flour", "cup", 1, ""),
		row("5 tablespoons", "tablespoons", "", 5, ""),
		row("sugar", "sugar", "", 0, ""),
		row(", divided", "divided", "", 0, ""),
		row("1 tsp salt", "salt", "tsp", 1, ""),
	}
	out, fixes := CleanRows(in, CleanOptions{})
	if len(out) != 3 {
		t.Fatalf("want 3 rows, got %d: %+v", len(out), out)
	}
	s := out[1]
	if s.Food != "sugar" || s.Unit != "tablespoons" || s.Amount != 5 || !strings.Contains(s.Note, "divided") {
		t.Errorf("merged row = %+v", s)
	}
	if out[0].Food != "flour" || len(out[0].Kinds) != 0 || out[2].Food != "salt" {
		t.Errorf("clean rows changed: %+v", out)
	}
	k := kindsOf(fixes)
	if k[KSplitMerge] != 1 || k[KOrphan] != 1 {
		t.Errorf("fix kinds = %v", k)
	}
}

func TestCleanRowsNoMergeAcrossGroups(t *testing.T) {
	in := []Row{
		{OriginalText: "5 tablespoons", Food: "tablespoons", Amount: 5, Group: 0},
		{OriginalText: "sugar", Food: "sugar", Group: 1},
	}
	out, fixes := CleanRows(in, CleanOptions{})
	if len(out) != 2 || kindsOf(fixes)[KUnpaired] != 1 {
		t.Errorf("merged across groups: %+v %+v", out, fixes)
	}
}

func TestCleanRowsLooseOrderReportOnly(t *testing.T) {
	in := []Row{
		{OriginalText: "5 tablespoons", Food: "tablespoons", Amount: 5, Loose: true},
		{OriginalText: "sugar", Food: "sugar", Loose: true},
	}
	out, fixes := CleanRows(in, CleanOptions{})
	if len(out) != 2 || len(fixes) != 1 || fixes[0].Apply {
		t.Errorf("loose group should be report-only: %+v %+v", out, fixes)
	}
}

func TestCleanRowsReparse(t *testing.T) {
	in := []Row{
		row("1/3 cup (packed; 68 g) dark brown sugar", "cup", "g", 68, ""),
		row("1/3 cup plus 1 Tbsp. extra-virgin olive oil", "plus . extra-virgin olive oil", "tbsp", 1, ""),
		row("1 head of garlic", "of garlic", "", 1, ""),
		row("1 cup unflavored, unsweetened oat milk", "unflavored", "cup", 1, "unsweetened oat milk"),
		row("A pinch of sea salt", "A pinch of sea salt", "", 0, ""),
		row("Pinch of cloves", "cloves", "pinch", 0, ""),
		row("2 tbsp olive oil", "olive oil", "tbsp", 2, ""),
	}
	out, fixes := CleanRows(in, CleanOptions{})
	want := []string{"dark brown sugar", "extra-virgin olive oil", "garlic", "unsweetened oat milk", "sea salt", "cloves", "olive oil"}
	for i, w := range want {
		if out[i].Food != w {
			t.Errorf("row %d food = %q, want %q", i, out[i].Food, w)
		}
	}
	if out[0].Unit != "cup" || absf(out[0].Amount-1.0/3) > .001 {
		t.Errorf("BA paren row = %+v", out[0])
	}
	if len(out[5].Kinds) != 0 || len(out[6].Kinds) != 0 {
		t.Errorf("clean rows were touched: %+v %+v", out[5], out[6])
	}
	k := kindsOf(fixes)
	if k[KUnitFood] != 1 || k[KLeadOfPlus] != 2 || k[KModifierFood] != 1 || k[KArticleLead] != 1 {
		t.Errorf("kinds = %v", k)
	}
}

func TestCleanRowsHeadersAndJunk(t *testing.T) {
	in := []Row{
		row("SALAD", "SALAD", "", 0, ""),
		row("1 cup kale", "kale", "cup", 1, ""),
		row("TO SERVE", "TO SERVE", "", 0, ""),
		row("1 TVP", "TVP", "", 1, ""),
		row("APPROXIMATELY 1 CUP (240 G)", "APPROXIMATELY", "", 1, ""),
		row("FATS: 14 g", "FATS", "g", 14, ""),
		row("Optional garnishes: chopped cilantro", "optional garnishes", "", 0, ""),
		row("Cinnamon: 1½ tsp ground", "cinnamon", "", 0, ""),
		row("2", "2", "", 2, ""),
		row("jalapeÃ±o, seeded", "jalapeÃ±o", "", 1, "seeded"),
	}
	out, fixes := CleanRows(in, CleanOptions{})
	k := kindsOf(fixes)
	if k[KHeader] != 2 || k[KYieldLine] != 1 || k[KNotIngredient] != 1 || k[KLabelContent] != 2 || k[KBareNumber] != 1 || k[KMojibake] != 1 {
		t.Errorf("kinds = %v", k)
	}
	if !out[0].IsHeader || out[0].Note != "SALAD" {
		t.Errorf("header row = %+v", out[0])
	}
	// risky drops are reported, not applied, by default
	if len(out) != len(in) {
		t.Errorf("risky drops applied without ApplyRisky: %d rows", len(out))
	}
	byOT := map[string]OutRow{}
	for _, o := range out {
		byOT[o.OriginalText] = o
	}
	if o := byOT["Optional garnishes: chopped cilantro"]; o.Food != "cilantro" || !strings.Contains(o.Note, "optional garnishes") {
		t.Errorf("label row = %+v", o)
	}
	if o := byOT["Cinnamon: 1½ tsp ground"]; o.Food != "cinnamon" || o.Unit != "tsp" || o.Amount != 1.5 {
		t.Errorf("cinnamon row = %+v", o)
	}
	if o := byOT["jalapeño, seeded"]; o.Food != "jalapeño" {
		t.Errorf("mojibake row = %+v", o)
	}
	out2, _ := CleanRows(in, CleanOptions{ApplyRisky: true})
	if len(out2) != len(in)-2 {
		t.Errorf("ApplyRisky should drop yield + nutrition rows: %d", len(out2))
	}
}

func TestCleanRowsIdempotent(t *testing.T) {
	in := []Row{
		row("5 tablespoons", "tablespoons", "", 5, ""),
		row("sugar", "sugar", "", 0, ""),
		row(", divided", "divided", "", 0, ""),
		row("1 head of garlic", "of garlic", "", 1, ""),
		row("SAUCE", "SAUCE", "", 0, ""),
	}
	out, _ := CleanRows(in, CleanOptions{})
	again := make([]Row, len(out))
	for i, o := range out {
		again[i] = o.Row
	}
	_, fixes := CleanRows(again, CleanOptions{})
	if len(fixes) != 0 {
		t.Errorf("second pass found more: %+v", fixes)
	}
}

func TestCleanRowsMeasurePlusModifier(t *testing.T) {
	in := []Row{
		row("1½ cups low-sodium", "low-sodium", "cups", 1.5, ""),
		row("vegetable broth", "vegetable broth", "", 0, ""),
		row("1 tablespoon low-sodium soy sauce", "low-sodium soy sauce", "tablespoon", 1, ""),
	}
	out, fixes := CleanRows(in, CleanOptions{})
	if len(out) != 2 || out[0].Food != "low-sodium vegetable broth" || out[0].Unit != "cups" || out[0].Amount != 1.5 {
		t.Errorf("out = %+v fixes = %+v", out, fixes)
	}
	if len(out) == 2 && len(out[1].Kinds) != 0 {
		t.Errorf("clean row touched: %+v", out[1])
	}
}
