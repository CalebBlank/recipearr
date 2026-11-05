// Command ingclean runs Recipearr's shared ingredient cleanup (enrich.CleanRows) over JSON on stdin,
// so other importers (cookbook-import's push.py, the library fixer) use exactly the same rules.
//
//	stdin:  {"apply_risky": false, "recipes": [{"key": "...", "rows": [Row...]}]}
//	stdout: {"recipes": [{"key": "...", "rows": [OutRow...], "fixes": [Fix...]}]}
//
// Row = {amount, unit, food, note, original_text, is_header, group, loose}. OutRow adds from (input
// indices) and kinds. Pure function: no network, no files.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"

	"recipearr/internal/enrich"
)

type inRecipe struct {
	Key  string       `json:"key"`
	Rows []enrich.Row `json:"rows"`
}

type outRecipe struct {
	Key   string          `json:"key"`
	Rows  []enrich.OutRow `json:"rows"`
	Fixes []enrich.Fix    `json:"fixes"`
}

func main() {
	var in struct {
		ApplyRisky bool       `json:"apply_risky"`
		Recipes    []inRecipe `json:"recipes"`
	}
	br := bufio.NewReader(os.Stdin)
	if b, err := br.Peek(3); err == nil && string(b) == "\xef\xbb\xbf" {
		br.Discard(3) // UTF-8 BOM (PowerShell pipes add one)
	}
	dec := json.NewDecoder(br)
	if err := dec.Decode(&in); err != nil {
		fmt.Fprintln(os.Stderr, "ingclean: bad input:", err)
		os.Exit(2)
	}
	out := struct {
		Recipes []outRecipe `json:"recipes"`
	}{Recipes: make([]outRecipe, 0, len(in.Recipes))}
	for _, r := range in.Recipes {
		rows, fixes := enrich.CleanRows(r.Rows, enrich.CleanOptions{ApplyRisky: in.ApplyRisky})
		if fixes == nil {
			fixes = []enrich.Fix{}
		}
		out.Recipes = append(out.Recipes, outRecipe{Key: r.Key, Rows: rows, Fixes: fixes})
	}
	w := bufio.NewWriter(os.Stdout)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(out); err != nil {
		fmt.Fprintln(os.Stderr, "ingclean:", err)
		os.Exit(1)
	}
	w.Flush()
}
