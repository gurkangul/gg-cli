package enforcement

import "testing"

func TestEnabled(t *testing.T) {
	cases := []struct {
		val  string
		want bool
	}{
		{"", false},
		{"0", false},
		{"false", false},
		{"random", false},
		{"1", true},
		{"true", true},
		{"TRUE", true},
		{"yes", true},
		{"ON", true},
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
