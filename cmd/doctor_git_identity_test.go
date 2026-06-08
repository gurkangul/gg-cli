package cmd

import "testing"

func TestIsAgentGitIdentity(t *testing.T) {
	cases := []struct {
		name, email string
		want        bool
	}{
		{"Hermes Agent", "hermes-agent@localhost", true},
		{"ci-bot", "ci@example.com", true},
		{"someone", "build-agent@users.noreply.github.com", true},
		{"", "user@localhost", true},
		{"Gurkan Gul", "gurkangul05@gmail.com", false},
		{"Jane Doe", "jane@company.com", false},
		{"Robert", "robert@robotics.com", false}, // "bot" inside "robotics" is not a word
	}
	for _, c := range cases {
		if got := isAgentGitIdentity(c.name, c.email); got != c.want {
			t.Errorf("isAgentGitIdentity(%q, %q) = %v, want %v", c.name, c.email, got, c.want)
		}
	}
}
