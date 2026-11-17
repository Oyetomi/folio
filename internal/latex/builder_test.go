package latex

import (
	"strings"
	"testing"
)

func TestPartPrefixStripped(t *testing.T) {
	cases := map[string]string{
		"Part I: Foundations":  "Foundations",
		"Part II — Practice":   "Practice",
		"part 3. Advanced":     "Advanced",
		"Part IV":              "",
		"Foundations":          "Foundations",
		"Partnerships in Code": "Partnerships in Code",
		"Particle Physics":     "Particle Physics",
	}
	for in, want := range cases {
		if got := strings.TrimSpace(rePartPrefix.ReplaceAllString(in, "")); got != want {
			t.Errorf("%q -> %q, want %q", in, got, want)
		}
	}
}

// Regression: case-only differences ("TCP" vs "Tcp") made the index order
// depend on map iteration, so CI builds of the same commit differed.
func TestIndexOrderDeterministic(t *testing.T) {
	direct := map[string][]string{
		"TCP": {"ch:1"}, "Tcp": {"ch:2"}, "tcp": {"ch:3"},
		"API": {"ch:1"}, "Api": {"ch:4"}, "Latency": {"ch:2"},
	}
	subs := map[string]map[string][]string{
		"Socket": {"Bind": {"ch:1"}, "bind": {"ch:2"}, "BIND": {"ch:3"}},
	}
	first := renderIndexSection(direct, subs)
	for i := 0; i < 200; i++ {
		// Rebuild the maps so their iteration order is reshuffled.
		d := map[string][]string{}
		for k, v := range direct {
			d[k] = v
		}
		s := map[string]map[string][]string{}
		for k, v := range subs {
			s[k] = v
		}
		if got := renderIndexSection(d, s); got != first {
			t.Fatalf("index output changed on iteration %d", i)
		}
	}
	if !(strings.Index(first, "API") < strings.Index(first, "Api")) {
		t.Error("ties should break on exact spelling (API before Api)")
	}
}
