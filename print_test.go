package main

import "testing"

func TestNormalizePageRanges(t *testing.T) {
	tests := []struct {
		input string
		want  string
		ok    bool
	}{
		{"", "", true},
		{" 1, 3-5, 9 ", "1,3-5,9", true},
		{"0", "", false},
		{"3-2", "", false},
		{"1-", "", false},
		{"1,,2", "", false},
		{"1;rm", "", false},
		{"-P1", "", false},
	}
	for _, tt := range tests {
		got, err := normalizePageRanges(tt.input)
		if (err == nil) != tt.ok || got != tt.want {
			t.Errorf("normalizePageRanges(%q) = %q, %v; want %q, ok=%v", tt.input, got, err, tt.want, tt.ok)
		}
	}
}
