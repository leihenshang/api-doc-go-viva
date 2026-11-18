package ime

import "testing"

func TestDisplayPartsMatchesVivasFileNaming(t *testing.T) {
	cases := map[string][2]string{
		"":       {"unix", "0"},
		":0":     {"unix", "0"},
		":0.0":   {"unix", "0"},
		":1.0":   {"unix", "1"},
		"host:2": {"host", "2"},
		"weird":  {"unix", "0"},
	}
	for display, want := range cases {
		host, number := displayParts(display)
		if host != want[0] || number != want[1] {
			t.Errorf("displayParts(%q) = (%q,%q)，期望 (%q,%q)", display, host, number, want[0], want[1])
		}
	}
}
