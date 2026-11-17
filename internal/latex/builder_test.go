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
