// Package feed polls RSS/Atom feeds and fetches page HTML for content acquisition.
package feed

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/mmcdole/gofeed"
)

// A browser-ish User-Agent gets past some naive scraper blocks (not the aggressive ones).
const browserUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0 Safari/537.36"

// Entry is one item discovered in a feed.
type Entry struct {
	GUID        string
	URL         string
	Title       string
	Categories  []string
	ContentHTML string // content:encoded if present, else the summary/description
	Published   time.Time
}

// Fetch parses a feed and returns its entries (newest as the feed orders them).
func Fetch(ctx context.Context, feedURL string) ([]Entry, error) {
	fp := gofeed.NewParser()
	fp.UserAgent = browserUA
	f, err := fp.ParseURLWithContext(feedURL, ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Entry, 0, len(f.Items))
	for _, it := range f.Items {
		link := cleanURL(strings.TrimSpace(it.Link))
		if link == "" {
			continue
		}
		e := Entry{
			GUID:        firstNonEmpty(strings.TrimSpace(it.GUID), link),
			URL:         link,
			Title:       strings.TrimSpace(it.Title),
			Categories:  it.Categories,
			ContentHTML: pickContent(it),
		}
		switch {
		case it.PublishedParsed != nil:
			e.Published = *it.PublishedParsed
		case it.UpdatedParsed != nil:
			e.Published = *it.UpdatedParsed
		}
		out = append(out, e)
	}
	return out, nil
}

func pickContent(it *gofeed.Item) string {
	if strings.TrimSpace(it.Content) != "" {
		return it.Content // content:encoded
	}
	return it.Description
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

var trackingParams = map[string]bool{
	"fbclid": true, "gclid": true, "mc_eid": true, "mc_cid": true,
	"ref": true, "source": true, "adt_ei": true, "igshid": true,
}

// cleanURL canonicalizes a recipe URL: drops the fragment and tracking query params (utm_*,
// mailchimp/social trackers). Stabilizes dedupe and avoids fetching campaign-tagged URLs.
func cleanURL(raw string) string {
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	u.Fragment = ""
	if u.RawQuery != "" {
		q := u.Query()
		for k := range q {
			lk := strings.ToLower(k)
			if strings.HasPrefix(lk, "utm_") || trackingParams[lk] {
				q.Del(k)
			}
		}
		u.RawQuery = q.Encode()
	}
	return u.String()
}

// HTTPError is returned by FetchHTML for non-2xx responses (e.g. anti-bot 402/403).
type HTTPError struct {
	Status int
	URL    string
}

func (e *HTTPError) Error() string { return fmt.Sprintf("fetch %s: HTTP %d", e.URL, e.Status) }

// FetchHTML downloads a page with a browser User-Agent. Used to acquire HTML to hand Tandoor
// as `data` (dodging its server-side anti-bot + URL-import rate limit). Capped at 5 MB.
func FetchHTML(ctx context.Context, pageURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", browserUA)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", &HTTPError{Status: resp.StatusCode, URL: pageURL}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 5<<20))
	if err != nil {
		return "", err
	}
	return string(body), nil
}
