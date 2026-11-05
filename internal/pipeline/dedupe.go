package pipeline

import (
	"context"
	"encoding/json"
	"net/url"
	"path"
	"strings"
	"unicode"

	"recipearr/internal/tandoor"
)

// Dedupe against what Tandoor already has, beyond Tandoor's own recipe-from-source "duplicates"
// (an exact-name match, so "Mama's" and "Mama’s" count as different recipes):
//
//  1. by link: an earlier Recipearr item for the same page (normalized URL) whose recipe Tandoor
//     still has. Catches an ad-hoc import of something a feed already brought in.
//  2. by name: Tandoor's search for the parsed name, compared after normalizing quotes, case and
//     punctuation, from the same site (its source_url's host).
//
// A duplicate records the existing recipe's id, so a caller (Mise's "Add from a URL") can open it.

// NormURL reduces a recipe link to what identifies the page: host without www, path without a
// trailing slash, the query minus tracking parameters, no fragment, no scheme.
func NormURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return strings.ToLower(strings.TrimSpace(raw))
	}
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	p := strings.TrimRight(u.EscapedPath(), "/")
	q := u.Query()
	for k := range q {
		lk := strings.ToLower(k)
		if strings.HasPrefix(lk, "utm_") || lk == "fbclid" || lk == "gclid" || lk == "ref" || lk == "mc_cid" || lk == "mc_eid" {
			q.Del(k)
		}
	}
	out := host + p
	if enc := q.Encode(); enc != "" {
		out += "?" + enc
	}
	return out
}

// NormName folds a recipe name for comparison: curly quotes and dashes to plain, lowercase, every
// run of non-letters/digits to one space.
func NormName(s string) string {
	var b strings.Builder
	space := false
	for _, r := range strings.ToLower(s) {
		switch r {
		case '’', '‘', '`', '´':
			continue // "mama’s" == "mamas" == "mama's"
		case '\'':
			continue
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if space && b.Len() > 0 {
				b.WriteByte(' ')
			}
			space = false
			b.WriteRune(r)
		} else {
			space = true
		}
	}
	return b.String()
}

// urlFrag is the part of a link used to narrow the item scan (its last path segment, else host).
func urlFrag(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return raw
	}
	if seg := path.Base(strings.TrimRight(u.Path, "/")); seg != "" && seg != "/" && seg != "." {
		return seg
	}
	return u.Hostname()
}

// existingByURL returns the id (and the earlier item's title) of a recipe Tandoor still has that an
// earlier item imported from the same page, or 0.
func (s *Service) existingByURL(ctx context.Context, tc *tandoor.Client, rawURL string) (int, string) {
	want := NormURL(rawURL)
	items, err := s.st.ItemsWithRecipeLike(urlFrag(rawURL))
	if err != nil {
		return 0, ""
	}
	checked := map[int64]bool{}
	for _, it := range items {
		if it.TandoorRecipeID == nil || checked[*it.TandoorRecipeID] || NormURL(it.URL) != want {
			continue
		}
		checked[*it.TandoorRecipeID] = true
		if ok, err := tc.RecipeExists(ctx, int(*it.TandoorRecipeID)); err == nil && ok {
			return int(*it.TandoorRecipeID), it.Title
		}
	}
	return 0, ""
}

// existingByName returns a recipe from the same site as rawURL whose name matches name once
// normalized, or nil. Same site only: cookbooks hold many recipes with common names ("Twice Baked
// Potatoes" in two books), and a web recipe of the same name is a different recipe.
func existingByName(ctx context.Context, tc *tandoor.Client, name, rawURL string) *tandoor.RecipeHit {
	want := NormName(name)
	site := siteOf(rawURL)
	if want == "" || site == "" {
		return nil
	}
	sameSite := func(id int) bool {
		raw, err := tc.GetRecipeRaw(ctx, id)
		if err != nil {
			return false
		}
		var r struct {
			SourceURL string `json:"source_url"`
		}
		return json.Unmarshal(raw, &r) == nil && siteOf(r.SourceURL) == site
	}
	// Tandoor's search is a substring match on the name, so "Mama's" won't find "Mama’s": after the
	// whole name, try its longest stretch of plain words.
	queries := []string{strings.TrimSpace(name)}
	if run := longestPlainRun(name); run != "" && !strings.EqualFold(run, queries[0]) {
		queries = append(queries, run)
	}
	for _, q := range queries {
		hits, err := tc.SearchRecipes(ctx, q, 25)
		if err != nil {
			return nil
		}
		for i := range hits {
			if NormName(hits[i].Name) == want && sameSite(hits[i].ID) {
				return &hits[i]
			}
		}
	}
	return nil
}

// siteOf is a link's host without www ("" when it has none: epub://, empty).
func siteOf(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return ""
	}
	return strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
}

// longestPlainRun is the longest part of s made of letters, digits and spaces only (trimmed).
func longestPlainRun(s string) string {
	best, cur := "", []rune{}
	flush := func() {
		if t := strings.TrimSpace(string(cur)); len(t) > len(best) {
			best = t
		}
		cur = cur[:0]
	}
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == ' ' {
			cur = append(cur, r)
		} else {
			flush()
		}
	}
	flush()
	return best
}
