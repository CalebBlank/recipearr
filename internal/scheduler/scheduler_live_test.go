package scheduler

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"recipearr/internal/pipeline"
	"recipearr/internal/store"
)

// TestRunDueLive exercises the actual scheduler tick path (runDue -> due -> Poll) — the auto-poll
// that backs "watch for new recipes." It uses a watch-only source so seeding imports NOTHING into
// Tandoor, and asserts the watch-only invariant: a scheduled poll of an unseeded source seeds it
// (marks entries skipped) rather than mass-importing. Skipped unless TANDOOR_URL/TOKEN are set.
func TestRunDueLive(t *testing.T) {
	baseURL := os.Getenv("TANDOOR_URL")
	token := os.Getenv("TANDOOR_TOKEN")
	if baseURL == "" || token == "" {
		t.Skip("set TANDOOR_URL/TANDOOR_TOKEN")
	}
	st, err := store.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	st.SetSetting("tandoor_url", baseURL)
	st.SetSetting("tandoor_token", token)

	src := &store.Source{
		Name: "Cookie and Kate", FeedURL: "https://cookieandkate.com/feed/",
		Enabled: true, PollIntervalMinutes: 60, BacklogOnAdd: false, BacklogLimit: 25,
	}
	if err := st.CreateSource(src); err != nil { // new => last_checked nil + seeded false => due
		t.Fatal(err)
	}

	sch := New(st, pipeline.New(st))
	sch.runDue(context.Background()) // fires the async poll goroutine

	// Wait for the goroutine to finish seeding.
	deadline := time.Now().Add(40 * time.Second)
	for time.Now().Before(deadline) {
		if s, _ := st.GetSource(src.ID); s != nil && s.Seeded {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}

	s, _ := st.GetSource(src.ID)
	if s == nil || !s.Seeded {
		t.Fatal("scheduler did not seed the source")
	}
	if s.LastCheckedAt == nil {
		t.Error("poll did not set last_checked_at")
	}

	items, _ := st.ListItems("", 200)
	imported, skipped := 0, 0
	for _, it := range items {
		switch it.Status {
		case store.StatusImported:
			imported++
		case store.StatusSkipped:
			skipped++
		}
	}
	t.Logf("scheduler seeded source: skipped=%d imported=%d seeded=%v", skipped, imported, s.Seeded)
	if imported != 0 {
		t.Errorf("watch-only seed imported %d recipes via scheduler, want 0", imported)
	}
	if skipped == 0 {
		t.Error("expected feed entries to be seeded as skipped")
	}
}
