package store

import (
	"fmt"
	"regexp"
	"strconv"
)

var taskIDRegex = regexp.MustCompile(`^TASK-\d{3,}$`)

// ParseTaskID extracts the numeric suffix from a task ID like "TASK-001".
func ParseTaskID(id string) (int, error) {
	if !taskIDRegex.MatchString(id) {
		return 0, fmt.Errorf("invalid task ID %q (expected TASK-NNN)", id)
	}
	return strconv.Atoi(id[5:])
}
