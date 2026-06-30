package llm

import "testing"

func TestExtractJSON(t *testing.T) {
	cases := []struct {
		name, input, want string
	}{
		{"raw", `{"k":"v"}`, `{"k":"v"}`},
		{"fenced json tag", "```json\n{\"k\":\"v\"}\n```", `{"k":"v"}`},
		{"fenced bare", "```\n{\"k\":\"v\"}\n```", `{"k":"v"}`},
		{"fenced no newline", "```json{\"k\":\"v\"}```", `{"k":"v"}`},
		{"surrounding prose", "Here you go:\n```json\n{\"k\":\"v\"}\n```\nHope that helps", `{"k":"v"}`},
		{"leading trailing space", "  {\"k\":\"v\"}  ", `{"k":"v"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ExtractJSON(tc.input); got != tc.want {
				t.Errorf("ExtractJSON(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}
