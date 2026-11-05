package pipeline

import "testing"

func TestNormURL(t *testing.T) {
	same := [][2]string{
		{"https://www.ambitiouskitchen.com/beef-chili/", "http://ambitiouskitchen.com/beef-chili"},
		{"https://www.noracooks.com/vegan-mac/?utm_source=rss&utm_medium=feed", "https://noracooks.com/vegan-mac#recipe"},
		{"https://example.com/r?id=12&fbclid=x", "https://example.com/r?id=12"},
	}
	for _, c := range same {
		if a, b := NormURL(c[0]), NormURL(c[1]); a != b {
			t.Errorf("NormURL %q = %q, %q = %q: want equal", c[0], a, c[1], b)
		}
	}
	diff := [][2]string{
		{"https://example.com/r?id=12", "https://example.com/r?id=13"},
		{"https://a.com/chili", "https://b.com/chili"},
	}
	for _, c := range diff {
		if NormURL(c[0]) == NormURL(c[1]) {
			t.Errorf("NormURL %q == %q: want different", c[0], c[1])
		}
	}
}

func TestNormName(t *testing.T) {
	cases := map[string]string{
		"Mama’s Beef Chili":                     "mamas beef chili",
		"Mama's  Beef-Chili":                    "mamas beef chili",
		"High-Protein Pumpkin ‘Cheesecake’ Jars": "high protein pumpkin cheesecake jars",
		"  BA’s Best Pumpkin Pie! ":              "bas best pumpkin pie",
	}
	for in, want := range cases {
		if got := NormName(in); got != want {
			t.Errorf("NormName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLongestPlainRun(t *testing.T) {
	cases := map[string]string{
		"Mama’s Beef Chili":                      "s Beef Chili",
		"High-Protein Pumpkin ‘Cheesecake’ Jars": "Protein Pumpkin",
		"Plain name":                             "Plain name",
	}
	for in, want := range cases {
		if got := longestPlainRun(in); got != want {
			t.Errorf("longestPlainRun(%q) = %q, want %q", in, got, want)
		}
	}
}
