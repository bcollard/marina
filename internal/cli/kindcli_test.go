package cli

import "testing"

func TestParseKindVersion(t *testing.T) {
	for in, want := range map[string]string{
		"kind v0.33.0 go1.26.3 linux/arm64\n": "v0.33.0",
		"":                                    "",
		"bash: kind: command not found":       "",
	} {
		if got := parseKindVersion(in); got != want {
			t.Errorf("parseKindVersion(%q) = %q, want %q", in, got, want)
		}
	}
}
