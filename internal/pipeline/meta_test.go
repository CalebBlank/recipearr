package pipeline

import (
	"testing"

	"recipearr/internal/tandoor"
)

func TestMetaDescription(t *testing.T) {
	cases := []struct{ html, want string }{
		{`<meta property="og:description" content="A lovely banana cake."/>`, "A lovely banana cake."},
		{`<meta content="Attributes flipped." name="description">`, "Attributes flipped."},
		{`<meta name="description" content="Plain meta desc.">`, "Plain meta desc."},
		{`<head><meta charset="utf-8"><meta property='og:description' content='Single quotes ok'></head>`, "Single quotes ok"},
		{`<p>no meta here</p>`, ""},
	}
	for _, c := range cases {
		if got := metaDescription(c.html); got != c.want {
			t.Errorf("metaDescription(%q) = %q, want %q", c.html, got, c.want)
		}
	}
}

func TestFillDescriptionFallback(t *testing.T) {
	rec := &tandoor.RFSRecipe{Description: ""}
	fillDescriptionFallback(rec, `<meta property="og:description" content="From the page meta.">`)
	if rec.Description != "From the page meta." {
		t.Errorf("empty description should be filled, got %q", rec.Description)
	}

	keep := &tandoor.RFSRecipe{Description: "The real recipe description."}
	fillDescriptionFallback(keep, `<meta property="og:description" content="should NOT win">`)
	if keep.Description != "The real recipe description." {
		t.Errorf("existing description must not be overridden, got %q", keep.Description)
	}
}
