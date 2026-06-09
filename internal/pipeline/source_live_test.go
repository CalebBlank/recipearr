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

// TestProcessSourceLive exercises the feed -> dedupe -> pre-filter -> import chain that the
// scheduler sits on top of. Skipped unless TANDOOR_URL/TANDOOR_TOKEN are set.
func TestProcessSourceLive(t *testing.T) {
	baseURL := os.Getenv("TANDOOR_URL")
	token := os.Getenv("TANDOOR_TOKEN")
	if baseURL == "" || token == "" {
		t.Skip("set TANDOOR_URL/TANDOOR_TOKEN")
	}
	const feedURL = "https://cookieandkate.com/feed/"

	newSvc := func(t *testing.T) (*Service, *store.Source) {
		st, err := store.Open(filepath.Join(t.TempDir(), "s.db"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { st.Close() })
		st.SetSetting("tandoor_url", baseURL)
		st.SetSetting("tandoor_token", token)
		src := &store.Source{Name: "Cookie and Kate", FeedURL: feedURL, Enabled: true, PollIntervalMinutes: 60, BacklogLimit: 25}
		if err := st.CreateSource(src); err != nil {
			t.Fatal(err)
		}
		return New(st), src
	}

	t.Run("seed mode writes no recipes", func(t *testing.T) {
		svc, src := newSvc(t)
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		res, err := svc.ProcessSource(ctx, src, ProcessOpts{Import: false})
		if err != nil {
			t.Fatal(err)
		}
		if res.Imported != 0 {
			t.Errorf("seed mode imported %d recipes, want 0", res.Imported)
		}
		if res.Skipped == 0 {
			t.Error("seed mode skipped 0 entries, expected the feed window")
		}
		t.Logf("seed: %+v", res)
	})

	t.Run("import one", func(t *testing.T) {
		svc, src := newSvc(t)
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		res, err := svc.ProcessSource(ctx, src, ProcessOpts{Import: true, Limit: 1})
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("import: %+v", res)

		imported, _ := svc.st.ListItems(store.StatusImported, 5)
		tc := tandoor.New(baseURL, token)
		for _, it := range imported {
			if it.TandoorRecipeID != nil {
				id := int(*it.TandoorRecipeID)
				t.Logf("imported %q -> recipe %d (cleaning up)", it.Title, id)
				if err := tc.DeleteRecipe(context.Background(), id); err != nil {
					t.Logf("cleanup delete %d: %v", id, err)
				}
			}
		}
		if res.Imported+res.FilteredOut+res.Duplicate+res.Failed == 0 {
			t.Error("import run acted on no entries")
		}
	})
}
