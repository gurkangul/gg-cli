package identity

import "testing"

func TestResolveAgent(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want string
	}{
		{
			name: "explicit unique agent is respected",
			env:  map[string]string{"GG_AGENT": "codex-1", "CLAUDE_SESSION_ID": "abcdef123456"},
			want: "codex-1",
		},
		{
			name: "generic claude-code + session id -> per-session id (BUG-084)",
			env:  map[string]string{"GG_AGENT": "claude-code", "CLAUDE_SESSION_ID": "abcdef1234567890"},
			want: "claude-code-abcdef12",
		},
		{
			name: "unset GG_AGENT + session id -> per-session id",
			env:  map[string]string{"CLAUDE_SESSION_ID": "short"},
			want: "claude-code-short",
		},
		{
			name: "generic claude-code without session id -> unchanged (no regression)",
			env:  map[string]string{"GG_AGENT": "claude-code"},
			want: "claude-code",
		},
		{
			name: "nothing set -> empty",
			env:  map[string]string{},
			want: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ResolveAgent(func(k string) string { return tc.env[k] })
			if got != tc.want {
				t.Errorf("ResolveAgent = %q, want %q", got, tc.want)
			}
		})
	}
}
