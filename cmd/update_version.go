package cmd

import (
	"strconv"
	"strings"
)

func compareSemver(a, b string) (int, bool) {
	av, apre, ok := parseSemverCore(a)
	if !ok {
		return 0, false
	}
	bv, bpre, ok := parseSemverCore(b)
	if !ok {
		return 0, false
	}
	for i := 0; i < 3; i++ {
		switch {
		case av[i] < bv[i]:
			return -1, true
		case av[i] > bv[i]:
			return 1, true
		}
	}
	switch {
	case apre == bpre:
		return 0, true
	case apre == "":
		return 1, true
	case bpre == "":
		return -1, true
	case apre < bpre:
		return -1, true
	default:
		return 1, true
	}
}

func parseSemverCore(s string) ([3]int, string, bool) {
	var out [3]int
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "gg version ")
	if !strings.HasPrefix(s, "v") {
		return out, "", false
	}
	s = strings.TrimPrefix(s, "v")
	if plus := strings.IndexByte(s, '+'); plus >= 0 {
		s = s[:plus]
	}
	pre := ""
	if dash := strings.IndexByte(s, '-'); dash >= 0 {
		pre = s[dash+1:]
		s = s[:dash]
	}
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return out, "", false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return out, "", false
		}
		out[i] = n
	}
	return out, pre, true
}

func envTruthy(v string) bool {
	v = strings.TrimSpace(strings.ToLower(v))
	if v == "" {
		return false
	}
	if b, err := strconv.ParseBool(v); err == nil {
		return b
	}
	switch v {
	case "yes", "y", "on":
		return true
	default:
		return false
	}
}
