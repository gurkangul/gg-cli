package enforcement

import "testing"

func TestEnabled(t *testing.T) {
	cases := []struct {
		val  string
		want bool
	}{
		// opt-out values
		{"off", false},
		{"OFF", false},
		{"0", false},
		{"false", false},
		{"FALSE", false},
		{"no", false},
		{"NO", false},
		// opt-in / default-on values
		{"", true},
		{"1", true},
		{"true", true},
		{"TRUE", true},
		{"yes", true},
		{"YES", true},
		{"on", true},
		{"ON", true},
		// unknown values default to enabled
		{"random", true},
	}
	for _, c := range cases {
		t.Run(c.val, func(t *testing.T) {
			t.Setenv(EnvVar, c.val)
			if got := Enabled(); got != c.want {
				t.Fatalf("Enabled()=%v, want %v (val=%q)", got, c.want, c.val)
			}
		})
	}
}
