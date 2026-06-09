package feed

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestFetchLive checks RSS parsing against a real feed. Skipped unless RECIPEARR_LIVE is set.
func TestFetchLive(t *testing.T) {
	if os.Getenv("RECIPEARR_LIVE") == "" {
		t.Skip("set RECIPEARR_LIVE to run live feed test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	entries, err := Fetch(ctx, "https://cookieandkate.com/feed/")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Fatal("no entries parsed")
	}
	t.Logf("parsed %d entries", len(entries))
	for i, e := range entries {
		if i >= 3 {
			break
		}
		t.Logf("[%d] title=%q url=%q categories=%v hasContent=%v",
			i, e.Title, e.URL, e.Categories, len(e.ContentHTML) > 0)
	}
}
