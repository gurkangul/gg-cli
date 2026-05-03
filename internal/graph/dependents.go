package graph

import (
	"context"
	"fmt"
)

// DependentHop records one downstream dependent and the hop distance from the
// original file. Hop 1 is equivalent to DependentsOf.
type DependentHop struct {
	Path string
	Hop  int
}

// DependentTraversal records bounded traversal metadata.
type DependentTraversal struct {
	Dependents     []DependentHop
	Cycles         []string
	Truncated      bool
	RequestedDepth int
	MaxDepth       int
}

// DependentsOfDepth returns downstream dependents up to maxDepth hops away.
// It deduplicates files reached through multiple paths and records cycle hits
// instead of returning duplicates.
func (c *Client) DependentsOfDepth(ctx context.Context, filePath string, maxDepth int) (DependentTraversal, error) {
	if maxDepth < 1 {
		return DependentTraversal{}, fmt.Errorf("dependents of %s depth: maxDepth must be >= 1", filePath)
	}

	traversal := DependentTraversal{
		RequestedDepth: maxDepth,
		MaxDepth:       maxDepth,
	}
	seen := map[string]int{filePath: 0}
	frontier := []string{filePath}
	cycleSeen := map[string]bool{}

	for hop := 1; hop <= maxDepth && len(frontier) > 0; hop++ {
		nextFrontier := make([]string, 0)
		for _, path := range frontier {
			deps, err := c.DependentsOf(ctx, path)
			if err != nil {
				return traversal, err
			}
			for _, dep := range deps {
				if dep == "" {
					continue
				}
				if previousHop, ok := seen[dep]; ok {
					if previousHop < hop && !cycleSeen[dep] {
						traversal.Cycles = append(traversal.Cycles, dep)
						cycleSeen[dep] = true
					}
					continue
				}
				seen[dep] = hop
				traversal.Dependents = append(traversal.Dependents, DependentHop{Path: dep, Hop: hop})
				nextFrontier = append(nextFrontier, dep)
			}
		}
		frontier = nextFrontier
	}

	if len(frontier) > 0 {
		traversal.Truncated = true
	}
	return traversal, nil
}
