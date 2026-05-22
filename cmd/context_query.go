package cmd

import (
	"fmt"

	"github.com/gurkangul/gg-cli/internal/contextartifacts"
	"github.com/gurkangul/gg-cli/internal/store"
)

type contextBundle struct {
	decisions   []store.Decision
	rejections  []store.Rejection
	tasks       []store.Task
	discussions []store.Discussion
	notes       []store.Note
	artifacts   []contextartifacts.Snippet
	sources     sourceLabels
	decErr      error
	rejErr      error
	taskErr     error
	discErr     error
	noteErr     error
	artifactErr error
}

// contextPayload is the cacheable form of a context bundle (exported fields only).
type contextPayload struct {
	Decisions   []store.Decision           `json:"decisions"`
	Rejections  []store.Rejection          `json:"rejections"`
	Tasks       []store.Task               `json:"tasks"`
	Discussions []store.Discussion         `json:"discussions"`
	Notes       []store.Note               `json:"notes"`
	Artifacts   []contextartifacts.Snippet `json:"artifacts"`
}

// taskContextBundle holds the result of a --for-task query.
type taskContextBundle struct {
	anchor   store.Task
	deps     []store.Task // tasks listed in anchor.DependsOn
	bundle   contextBundle
	messages []store.Message
}

// collectBundleErrors returns a slice of warning strings for any non-nil errors in bundle.
func collectBundleErrors(bundle contextBundle) []string {
	var errs []string
	if bundle.decErr != nil {
		errs = append(errs, fmt.Sprintf("decisions: %v", bundle.decErr))
	}
	if bundle.rejErr != nil {
		errs = append(errs, fmt.Sprintf("rejections: %v", bundle.rejErr))
	}
	if bundle.taskErr != nil {
		errs = append(errs, fmt.Sprintf("tasks: %v", bundle.taskErr))
	}
	if bundle.discErr != nil {
		errs = append(errs, fmt.Sprintf("discussions: %v", bundle.discErr))
	}
	if bundle.noteErr != nil {
		errs = append(errs, fmt.Sprintf("notes: %v", bundle.noteErr))
	}
	if bundle.artifactErr != nil {
		errs = append(errs, fmt.Sprintf("artifacts: %v", bundle.artifactErr))
	}
	return errs
}

// boostRelatedDecisions moves decisions whose TaskID matches the anchor or
// whose tags overlap to the front without duplicating them.
func boostRelatedDecisions(decisions []store.Decision, anchorID string, anchorTags map[string]bool) []store.Decision {
	var pinned, rest []store.Decision
	for _, dec := range decisions {
		if dec.TaskID == anchorID {
			pinned = append(pinned, dec)
			continue
		}
		for _, tag := range dec.Tags {
			if anchorTags[tag] {
				pinned = append(pinned, dec)
				goto next
			}
		}
		rest = append(rest, dec)
	next:
	}
	return append(pinned, rest...)
}
