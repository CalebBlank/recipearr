package enrich

import "strings"

// knownUnits is a generous allowlist of real measurement/count units. Anything a scraper
// labels as a "unit" that isn't in here (e.g. "guajillo", "flour", "large") is almost certainly
// part of the food name and gets folded back into it.
var knownUnits = func() map[string]bool {
	list := []string{
		"cup", "cups", "c",
		"tablespoon", "tablespoons", "tbsp", "tbsps", "tbs", "tbl", "t", "T",
		"teaspoon", "teaspoons", "tsp", "tsps", "ts",
		"ounce", "ounces", "oz", "fluid ounce", "fluid ounces", "fl oz", "floz",
		"pound", "pounds", "lb", "lbs",
		"gram", "grams", "g", "gr",
		"kilogram", "kilograms", "kg",
		"milligram", "milligrams", "mg",
		"milliliter", "milliliters", "millilitre", "millilitres", "ml",
		"liter", "liters", "litre", "litres", "l",
		"pinch", "pinches", "dash", "dashes", "smidgen", "drop", "drops",
		"clove", "cloves", "can", "cans", "jar", "jars",
		"package", "packages", "pkg", "pkgs", "packet", "packets",
		"stick", "sticks", "slice", "slices", "sprig", "sprigs",
		"bunch", "bunches", "head", "heads", "piece", "pieces",
		"quart", "quarts", "qt", "qts", "pint", "pints", "pt", "pts",
		"gallon", "gallons", "gal", "stalk", "stalks",
		"fillet", "fillets", "filet", "filets", "strip", "strips",
		"cube", "cubes", "handful", "handfuls", "knob", "knobs",
		"scoop", "scoops", "bottle", "bottles", "box", "boxes", "bag", "bags",
		"ear", "ears", "rib", "ribs", "wedge", "wedges", "segment", "segments",
		"leaf", "leaves", "sheet", "sheets", "tin", "tins",
		"container", "containers", "glass", "glasses", "loaf", "loaves",
		"to taste",
	}
	m := make(map[string]bool, len(list))
	for _, u := range list {
		m[strings.ToLower(u)] = true
	}
	return m
}()

// isKnownUnit reports whether name is a recognized measurement/count unit.
func isKnownUnit(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	n = strings.TrimRight(n, ".")
	n = strings.Join(strings.Fields(n), " ")
	if n == "" {
		return false
	}
	return knownUnits[n]
}
