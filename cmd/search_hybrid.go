package cmd

import (
	"strings"

	"github.com/gurkangul/gg-cli/internal/brain"
	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/store"
)

// search_hybrid.go — TASK-516 always-on lexical tier over BRAIN RECORDS.
//
// gg search is semantic-vector first. The lexical JSONL scan
// (brain.SearchByTextScored) already existed but only ever ran as a break-glass
// fallback inside serveSearchFromJSONL — i.e. when the store is down or the
// collection is missing. On the healthy path the only lexical tier was
// lexicalSymbolMatches (TASK-505), which covers CODE SYMBOLS, not brain records.
//
// That left a silent recall hole: a decision/task/bug whose text contains the
// query verbatim can be absent from the vector result set entirely — it was
// written while the embedder was down (JSONL + outbox, never embedded), it
// carries a degraded zero-vector that Search* filters out, or it simply ranked
// below the semantic cutoff. Search then printed "No results found." with no
// error, which an agent reads as "this was never decided" and re-decides.
//
// hybridCandidates closes that hole by promoting the lexical scan to a
// first-class, always-on tier: it unions lexically-matching records into the
// vector candidate set, deduped by record ID. Ranking is unchanged and stays
// vector-primary — rankSearchResults already sorts by lexical score first and
// breaks ties on SemanticScore, so a record found by BOTH tiers (semantic score
// > 0) outranks a lexical-only record (semantic score 0) at equal lexical score.
//
// Status filtering MUST mirror the vector path exactly. The JSONL scan is
// unfiltered, so appending its hits blindly would resurrect superseded/rejected
// decisions and fixed/wontfix bugs into search output — precisely the
// regression BUG-064 fixed. The active* predicates below mirror
// ActiveDecisionsFilter / ActiveBugsFilter / ActiveTasksFilter.

// hybridCandidates augments the vector-search candidate slices with brain
// records that match the query lexically but that the vector tier missed.
//
// It is fully best-effort: a missing .gg dir or an unreadable JSONL kind yields
// the inputs unchanged and never fails search. perKind bounds how many extra
// records each kind may contribute so a broad query cannot flood the candidate
// set (the caller passes the same over-fetch bound the vector tier uses).
func hybridCandidates(
	query string,
	decisions []store.Decision,
	rejections []store.Rejection,
	tasks []store.Task,
	bugs []store.Bug,
	notes []store.Note,
	messages []store.Message,
	includeAll bool,
	perKind int,
) ([]store.Decision, []store.Rejection, []store.Task, []store.Bug, []store.Note, []store.Message) {
	ggDir := config.GGDirOrEmpty()
	if ggDir == "" || strings.TrimSpace(query) == "" || perKind <= 0 {
		return decisions, rejections, tasks, bugs, notes, messages
	}

	seen := func(ids map[string]bool, id string) bool {
		if id == "" || ids[id] {
			return true
		}
		ids[id] = true
		return false
	}

	if matches, err := brain.SearchByTextScored(ggDir, "decisions", query); err == nil {
		ids := make(map[string]bool, len(decisions))
		for _, d := range decisions {
			ids[d.ID] = true
		}
		added := 0
		for _, m := range matches {
			if added >= perKind {
				break
			}
			d := decisionFromJSONLEntry(m.Entry)
			if seen(ids, d.ID) || !hybridDecisionVisible(d.Status, includeAll) {
				continue
			}
			decisions = append(decisions, d)
			added++
		}
	}

	if matches, err := brain.SearchByTextScored(ggDir, "rejections", query); err == nil {
		ids := make(map[string]bool, len(rejections))
		for _, r := range rejections {
			ids[r.ID] = true
		}
		added := 0
		for _, m := range matches {
			if added >= perKind {
				break
			}
			r := rejectionFromJSONLEntry(m.Entry)
			if seen(ids, r.ID) {
				continue
			}
			rejections = append(rejections, r)
			added++
		}
	}

	if matches, err := brain.SearchByTextScored(ggDir, "tasks", query); err == nil {
		ids := make(map[string]bool, len(tasks))
		for _, t := range tasks {
			ids[t.ID] = true
		}
		added := 0
		for _, m := range matches {
			if added >= perKind {
				break
			}
			t := taskFromJSONLEntry(m.Entry)
			if seen(ids, t.ID) || !hybridTaskVisible(t.Status) {
				continue
			}
			tasks = append(tasks, t)
			added++
		}
	}

	if matches, err := brain.SearchByTextScored(ggDir, "bugs", query); err == nil {
		ids := make(map[string]bool, len(bugs))
		for _, b := range bugs {
			ids[b.ID] = true
		}
		added := 0
		for _, m := range matches {
			if added >= perKind {
				break
			}
			b := bugFromJSONLEntry(m.Entry)
			if seen(ids, b.ID) || !hybridBugVisible(b.Status, includeAll) {
				continue
			}
			bugs = append(bugs, b)
			added++
		}
	}

	if matches, err := brain.SearchByTextScored(ggDir, "notes", query); err == nil {
		ids := make(map[string]bool, len(notes))
		for _, n := range notes {
			ids[n.ID] = true
		}
		added := 0
		for _, m := range matches {
			if added >= perKind {
				break
			}
			n := noteFromJSONLEntry(m.Entry)
			if seen(ids, n.ID) {
				continue
			}
			notes = append(notes, n)
			added++
		}
	}

	// Messages are lexical-ONLY: the store has no SearchMessages, so on the
	// healthy path they were never queried at all — a handoff captured in a
	// `gg tell` was findable when the vector store was DOWN (the JSONL fallback
	// does scan messages) and invisible when it was healthy. The command's own
	// help has always promised "semantic search across decisions, tasks, and
	// messages"; this is what makes that true.
	if matches, err := brain.SearchByTextScored(ggDir, "messages", query); err == nil {
		ids := make(map[string]bool, len(messages))
		for _, m := range messages {
			ids[m.ID] = true
		}
		added := 0
		for _, m := range matches {
			if added >= perKind {
				break
			}
			msg := messageFromJSONLEntry(m.Entry)
			if seen(ids, msg.ID) {
				continue
			}
			messages = append(messages, msg)
			added++
		}
	}

	return decisions, rejections, tasks, bugs, notes, messages
}

// hybridDecisionVisible mirrors ActiveDecisionsFilter: superseded and rejected
// decisions stay suppressed unless --include-superseded was passed. An empty
// status is legacy-active (it is definitionally neither superseded nor
// rejected), so it stays visible rather than being silently dropped.
func hybridDecisionVisible(status string, includeAll bool) bool {
	if includeAll {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "superseded", "rejected":
		return false
	default:
		return true
	}
}

// hybridBugVisible mirrors ActiveBugsFilter (open/fixing/reopened): resolved
// bugs must not surface as current context unless --include-superseded.
func hybridBugVisible(status string, includeAll bool) bool {
	if includeAll {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "fixed", "wontfix":
		return false
	default:
		return true
	}
}

// hybridTaskVisible mirrors ActiveTasksFilter: runSearch always queries tasks
// with includeAll=false, so only pending/in_progress tasks are candidates.
func hybridTaskVisible(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "pending", "in_progress":
		return true
	default:
		return false
	}
}
