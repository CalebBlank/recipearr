package pipeline

import (
	"context"
	"encoding/json"
	"html"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"recipearr/internal/enrich"
	"recipearr/internal/tandoor"
)

// TestDebugTransformLive dumps the enriched create payload and surfaces the server's specific
// error if the create fails. Skipped unless TANDOOR_URL/TANDOOR_TOKEN are set.
func TestDebugTransformLive(t *testing.T) {
	baseURL := os.Getenv("TANDOOR_URL")
	token := os.Getenv("TANDOOR_TOKEN")
	if baseURL == "" || token == "" {
		t.Skip("set TANDOOR_URL/TANDOOR_TOKEN")
	}
	tc := tandoor.New(baseURL, token)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	rfs, msg := parseRecipe(ctx, tc, "https://cookieandkate.com/best-guacamole-recipe/")
	if rfs == nil {
		t.Fatal("parse failed: " + msg)
	}
	payload := enrich.Transform(rfs.Recipe, enrich.DefaultOptions())
	b, _ := json.MarshalIndent(payload, "", "  ")
	out := filepath.Join(os.TempDir(), "recipearr_payload.json")
	_ = os.WriteFile(out, b, 0o644)
	t.Logf("payload written to %s (%d bytes)", out, len(b))

	id, err := tc.CreateRecipe(ctx, payload)
	if err != nil {
		if apiErr, ok := err.(*tandoor.APIError); ok {
			t.Logf("CREATE FAILED HTTP %d; exception: %s", apiErr.Status, exceptionValue(apiErr.Body))
		} else {
			t.Logf("CREATE FAILED: %v", err)
		}
		return
	}
	t.Logf("created recipe %d", id)
	_ = tc.DeleteRecipe(ctx, id)
}

func exceptionValue(body string) string {
	const marker = `class="exception_value">`
	i := strings.Index(body, marker)
	if i < 0 {
		if len(body) > 300 {
			return body[:300]
		}
		return body
	}
	rest := body[i+len(marker):]
	if j := strings.Index(rest, "</pre>"); j >= 0 {
		return html.UnescapeString(strings.TrimSpace(rest[:j]))
	}
	return rest
}
