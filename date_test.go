package main

import "testing"

func TestFriendlyDate(t *testing.T) {
	cases := map[string]string{
		"2025-09-01": "September 1st, 2025", "2025-09-02": "September 2nd, 2025", "2025-09-03": "September 3rd, 2025", "2025-09-11": "September 11th, 2025", "2025-09-12": "September 12th, 2025", "2025-09-13": "September 13th, 2025", "2025-09-21": "September 21st, 2025", "invalid": "invalid",
	}
	for input, want := range cases {
		if got := friendlyDate(input); got != want {
			t.Errorf("friendlyDate(%q) = %q; want %q", input, got, want)
		}
	}
}
