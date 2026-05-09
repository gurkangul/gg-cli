package cmd

import "fmt"

func pct(part, total int) int {
	if total == 0 {
		return 0
	}
	return (part * 100) / total
}

func fmtCount(n uint64, err error) string {
	if err != nil {
		return "?"
	}
	return fmt.Sprintf("%d", n)
}
