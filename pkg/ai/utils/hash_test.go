package utils

import "testing"

func TestShortHashMatchesTypeScript(t *testing.T) {
	cases := map[string]string{
		"":                             "k4n83c7h0j2b",
		"abc":                          "y0biex7f9bbh",
		"fc_item_1":                    "eur4c4ubu0zl",
		"a very long response item id": "uajp4l1zibk9",
	}
	for input, want := range cases {
		if got := ShortHash(input); got != want {
			t.Fatalf("ShortHash(%q) = %q, want %q", input, got, want)
		}
	}
}
