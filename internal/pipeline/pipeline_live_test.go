package pipeline

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"recipearr/internal/store"
	"recipearr/internal/tandoor"
)

// TestImportURL_Live runs the full pipeline against a real Tandoor. It is skipped unless
// TANDOOR_URL and TANDOOR_TOKEN are set. It imports a recipe, logs the enriched result for
// inspection, then deletes it so the instance is left clean.
func TestImportURL_Live(t *testing.T) {
	baseURL := os.Getenv("TANDOOR_URL")
	token := os.Getenv("TANDOOR_TOKEN")
	if baseURL == "" || token == "" {
		t.Skip("set TANDOOR_URL/TANDOOR_TOKEN to run the live e2e test")
	}

	st, err := store.Open(filepath.Join(t.TempDir(), "e2e.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.SetSetting("tandoor_url", baseURL); err != nil {
		t.Fatal(err)
	}
	if err := st.SetSetting("tandoor_token", token); err != nil {
		t.Fatal(err)
	}

	svc := New(st)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	const recipeURL = "https://cookieandkate.com/best-guacamole-recipe/"
	it, err := svc.ImportURL(ctx, recipeURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	if it.Status != store.StatusImported {
		t.Fatalf("import status=%s reason=%q error=%q", it.Status, it.FilterReason, it.Error)
	}
	if it.TandoorRecipeID == nil {
		t.Fatal("imported but no tandoor recipe id recorded")
	}

	tc := tandoor.New(baseURL, token)
	id := int(*it.TandoorRecipeID)
	defer func() {
		if err := tc.DeleteRecipe(context.Background(), id); err != nil {
			t.Logf("cleanup: failed to delete recipe %d: %v", id, err)
		} else {
			t.Logf("cleanup: deleted recipe %d", id)
		}
	}()

	raw, err := tc.GetRecipeRaw(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("IMPORTED recipe id=%d title=%q raw_tags=%q", id, it.Title, it.RawTags)
	t.Logf("CREATED RECIPE JSON:\n%s", string(raw))
}
