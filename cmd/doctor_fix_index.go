package cmd

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/graph"
)

func runDoctorFixIndex(cmd *cobra.Command) error {
	cfg, err := config.Load()
	if err != nil {
		return configErr(fmt.Sprintf("load config: %v", err))
	}
	root, err := config.FindRoot()
	if err != nil {
		return configErr(err.Error())
	}
	ggDir := filepath.Join(root, config.DirName)

	// TASK-473 / BUG-088: heal the brain relationship graph too. The code-graph
	// refresh below can early-return when already fresh, so reconcile the brain
	// (Decision/Task nodes + DECIDES/DEPENDS_ON edges) first, on every explicit
	// repair. Reuses the proven reindex paths; best-effort — failures warn, never
	// block the code-graph repair.
	fmt.Println("Reconciling brain graph (tasks → decisions)…")
	if rErr := runTaskReindex(cmd, nil); rErr != nil {
		fmt.Printf("warning: brain task reconcile: %v\n", rErr)
	}
	if rErr := runBrainReindexDecisions(cmd, nil); rErr != nil {
		fmt.Printf("warning: brain decision reconcile: %v\n", rErr)
	}

	statusCtx, statusCancel := withTimeout(cmd.Context())
	status := collectCodeGraphStatus(statusCtx, root, ggDir, cfg)
	statusCancel()

	if status.Status == "ready" {
		fmt.Printf("code graph already fresh: %s\n", status.Detail)
		return nil
	}
	if status.Status == "not_applicable" {
		fmt.Printf("code graph not applicable: %s\n", status.Detail)
		return nil
	}
	if err := doctorFixIndexCheckServices(cmd.Context(), cfg, ggDir); err != nil {
		return err
	}

	langs := langsFromNames(status.DetectedLanguages)
	if len(langs) == 0 {
		var detectErr error
		var hasSource bool
		langs, hasSource, detectErr = detectCodeGraphLanguages(root)
		if detectErr != nil {
			return fmt.Errorf("detect code graph languages: %w", detectErr)
		}
		if len(langs) == 0 && hasSource {
			return fmt.Errorf("supported source files found but no indexable module was found; add a supported module manifest (go.mod, package.json, tsconfig.json, pyproject.toml, setup.py, requirements.txt, Package.swift, or an Xcode project/workspace) and re-run gg doctor --fix-index")
		}
	}
	if len(langs) == 0 {
		return fmt.Errorf("no supported code modules or source files found; nothing to index")
	}

	for _, lang := range langs {
		fullIndex := shouldUseFullIndexForFix(status)
		if fullIndex {
			fmt.Printf("Refreshing code graph: gg index --lang %s\n", lang)
		} else {
			fmt.Printf("Refreshing code graph: gg index --lang %s --changed\n", lang)
		}
		if err := runIndexOnce(cmd, lang, !fullIndex); err != nil {
			return err
		}
	}

	verifyCtx, verifyCancel := withTimeout(cmd.Context())
	after := collectCodeGraphStatus(verifyCtx, root, ggDir, cfg)
	verifyCancel()
	if after.Status != "ready" {
		detail := after.Detail
		if detail == "" {
			detail = after.Status
		}
		return fmt.Errorf("code graph refresh finished but status is %s: %s", after.Status, detail)
	}
	fmt.Printf("✓ code graph refreshed: %s\n", after.Detail)
	return nil
}

func shouldUseFullIndexForFix(status codeGraphStatus) bool {
	if status.Status != "missing" {
		return false
	}
	if status.GraphEmpty || !status.GraphStatsAvailable || !status.MemgraphAvailable {
		return true
	}
	return status.MemgraphDetail == "not checked"
}

func doctorFixIndexCheckServices(parent context.Context, cfg *config.Config, ggDir string) error {
	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()

	if qc, err := doctorQdrantNewClient(cfg, ggDir); err != nil {
		fmt.Printf("warning: vector store unavailable (%v); continuing with code-graph refresh\n", err)
	} else {
		defer func() { _ = qc.Close() }()
		if err := qc.HealthCheck(ctx); err != nil {
			fmt.Printf("warning: vector store unavailable (%v); continuing with code-graph refresh\n", err)
		}
	}

	gc, err := graph.New(cfg.DataDir, cfg.ProjectID)
	if err != nil {
		return serviceErr(fmt.Sprintf("code graph client: %v", err))
	}
	defer func() { _ = gc.Close(ctx) }()
	if err := gc.HealthCheck(ctx); err != nil {
		return memgraphDownErr("code graph unavailable", err)
	}
	return nil
}
