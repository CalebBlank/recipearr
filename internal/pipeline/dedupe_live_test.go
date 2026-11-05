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

// TestDedupe_Live checks both dedupe paths against a real Tandoor, read only (nothing is created).
// Skipped unless TANDOOR_URL, TANDOOR_TOKEN and DEDUPE_RECIPE_ID / DEDUPE_RECIPE_URL /
// DEDUPE_RECIPE_NAME (an existing recipe, its link and its name typed with plain quotes) are set.
func TestDedupe_Live(t *testing.T) {
	baseURL, token := os.Getenv("TANDOOR_URL"), os.Getenv("TANDOOR_TOKEN")
	rid, rurl, rname := os.Getenv("DEDUPE_RECIPE_ID"), os.Getenv("DEDUPE_RECIPE_URL"), os.Getenv("DEDUPE_RECIPE_NAME")
	if baseURL == "" || token == "" || rid == "" || rurl == "" || rname == "" {
		t.Skip("set TANDOOR_URL/TANDOOR_TOKEN/DEDUPE_RECIPE_* to run")
	}
	var id int64
	for _, c := range rid {
		id = id*10 + int64(c-'0')
	}
	st, err := store.Open(filepath.Join(t.TempDir(), "dedupe.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	gone := int64(999999999)
	_ = st.CreateItem(&store.Item{URL: rurl, Status: store.StatusImported, TandoorRecipeID: &id})
	_ = st.CreateItem(&store.Item{URL: "https://example.org/deleted-recipe/", Status: store.StatusImported, TandoorRecipeID: &gone})
	svc := New(st)
	tc := tandoor.New(baseURL, token)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	if got := svc.existingByURL(ctx, tc, rurl+"?utm_source=rss#recipe"); int64(got) != id {
		t.Errorf("existingByURL(same link) = %d, want %d", got, id)
	}
	if got := svc.existingByURL(ctx, tc, "https://www.example.org/deleted-recipe"); got != 0 {
		t.Errorf("existingByURL(deleted recipe) = %d, want 0", got)
	}
	if got := svc.existingByURL(ctx, tc, "https://example.org/never-seen/"); got != 0 {
		t.Errorf("existingByURL(new link) = %d, want 0", got)
	}
	if hit := existingByName(ctx, tc, rname); hit == nil || int64(hit.ID) != id {
		t.Errorf("existingByName(%q) = %+v, want id %d", rname, hit, id)
	}
	if hit := existingByName(ctx, tc, "Zz No Such Recipe Qq"); hit != nil {
		t.Errorf("existingByName(nonsense) = %+v, want nil", hit)
	}
}
