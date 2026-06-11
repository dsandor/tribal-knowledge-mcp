package tags

import (
	"reflect"
	"testing"
)

func TestExtractHashtags(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"simple", "use #earnings for this", []string{"earnings"}},
		{"multiple", "#Q3-Report and #earnings_call", []string{"q3-report", "earnings_call"}},
		{"lowercased", "#Earnings #EARNINGS", []string{"earnings"}},
		{"dedup", "#alpha #alpha #beta", []string{"alpha", "beta"}},
		{"mid-word hash ignored", "foo#bar", nil},
		{"bare hash ignored", "a # b #! c", nil},
		{"leading punctuation stops", "#-nope but #ok-tag yes", []string{"ok-tag"}},
		{"start of string", "#first word", []string{"first"}},
		{"empty input", "", nil},
		{"hash at end", "trailing #", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ExtractHashtags(c.in)
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("ExtractHashtags(%q) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

func TestMerge(t *testing.T) {
	cases := []struct {
		name      string
		explicit  []string
		extracted []string
		want      []string
	}{
		{"both empty", nil, nil, []string{}},
		{"explicit only", []string{"Alpha", "beta"}, nil, []string{"alpha", "beta"}},
		{"extracted only", nil, []string{"gamma"}, []string{"gamma"}},
		{"dedup across sources", []string{"Alpha"}, []string{"alpha", "beta"}, []string{"alpha", "beta"}},
		{"order preserved explicit first", []string{"b", "a"}, []string{"c"}, []string{"b", "a", "c"}},
		{"whitespace dropped", []string{" ", "a"}, []string{""}, []string{"a"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Merge(c.explicit, c.extracted)
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("Merge(%v, %v) = %v, want %v", c.explicit, c.extracted, got, c.want)
			}
		})
	}
}
