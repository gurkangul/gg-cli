package cmd

import (
	"fmt"
	"net"
	"time"

	"github.com/spf13/cobra"
)

// sandboxProbeTarget is the default TCP address used by --diagnose-sandbox.
// We probe localhost:6334 (Qdrant's default port) because what matters is
// whether any TCP socket to localhost is permitted, not whether Qdrant is up.
const sandboxProbeTarget = "localhost:6334"

// runDoctorDiagnoseSandbox probes a known localhost TCP target and reports
// whether the sandbox permits outbound TCP to localhost.
func runDoctorDiagnoseSandbox(cmd *cobra.Command) error {
	fmt.Println("GG Doctor — diagnose sandbox")
	fmt.Println("─────────────────────────────────────────────────")
	fmt.Printf("  Probing TCP %s ...\n", sandboxProbeTarget)

	conn, err := net.DialTimeout("tcp", sandboxProbeTarget, 3*time.Second)
	if err != nil {
		if isSandboxPermissionError(err) {
			fmt.Fprintf(cmd.OutOrStdout(), "  sandbox: TCP localhost BLOCKED — escalate permissions\n")
			fmt.Fprintf(cmd.OutOrStdout(), "  → error: %v\n", err)
			return nil
		}
		// Connection refused or timeout means TCP is permitted but the service is not listening.
		fmt.Fprintf(cmd.OutOrStdout(), "  sandbox: TCP localhost permitted (service not running at %s)\n", sandboxProbeTarget)
		return nil
	}
	_ = conn.Close()
	fmt.Fprintf(cmd.OutOrStdout(), "  sandbox: TCP localhost permitted\n")
	return nil
}
