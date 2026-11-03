package everlore

import "testing"

func TestValidWarnings(t *testing.T) {
	for _, test := range []struct {
		values []string
		valid  bool
	}{
		{nil, false}, {[]string{"Incest"}, true}, {[]string{"Incest", "Underage sex"}, true},
		{[]string{"None apply"}, true}, {[]string{"Sexual content", "Dub-con"}, true},
		{[]string{"Incest", "None apply"}, false}, {[]string{"Non-con", "Graphic violence"}, true},
		{[]string{"Incest", "Incest"}, false}, {[]string{"unknown"}, false},
	} {
		if got := validWarnings(test.values); got != test.valid {
			t.Errorf("validWarnings(%v) = %v", test.values, got)
		}
	}
}
