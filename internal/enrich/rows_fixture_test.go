package enrich

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestCleanRowsOnFixtures runs CleanRows over every captured recipe-from-source fixture and logs
// each fix, so a rule change shows exactly what it would do to real imports (go test -v -run Fixtures).
// Every fix must be a named kind and applied fixes must leave a clean food.
func TestCleanRowsOnFixtures(t *testing.T) {
	files, _ := filepath.Glob("../../testdata_rfs_*.json")
	if len(files) == 0 {
		t.Skip("no fixtures")
	}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var fx fixture
		if err := json.Unmarshal(b, &fx); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		var rows []Row
		for si, s := range fx.Steps {
			for _, ing := range s.Ingredients {
				rows = append(rows, Row{Amount: ing.Amount, Unit: ing.Unit, Food: ing.Food, Note: ing.Note, OriginalText: ing.OriginalText, Group: si})
			}
		}
		_, fixes := CleanRows(rows, CleanOptions{})
		for _, fx := range fixes {
			after := "(none)"
			if fx.After != nil {
				after = fx.After.Food + " | " + fx.After.Unit + " | " + fx.After.Note
				if fx.Apply && !fx.After.IsHeader && !CleanFood(fx.After.Food) {
					t.Errorf("%s: applied fix left an unclean food %q", f, fx.After.Food)
				}
			}
			t.Logf("%s %s apply=%v %q -> %s", filepath.Base(f), fx.Kind, fx.Apply, fx.Before[0].OriginalText, after)
		}
	}
}
