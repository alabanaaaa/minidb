package cli

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
)

var activeWorker string

var sessionCmd = &cobra.Command{
	Use:   "session",
	Short: "Manage worker sessions",
}

var sessionStartCmd = &cobra.Command{
	Use:   "start [workerID]",
	Short: "Start a worker session",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		if activeWorker != "" {
			fmt.Println("A session is already active. End it first.")
			return
		}

		activeWorker = args[0]
		fmt.Printf("Session started for worker: %s\n", activeWorker)
	},
}

var sessionEndCmd = &cobra.Command{
	Use:   "end",
	Short: "End active session",
	Run: func(cmd *cobra.Command, args []string) {
		if activeWorker == "" {
			fmt.Println("No active session.")
			return
		}

		fmt.Printf("Session ended for worker: %s\n", activeWorker)
		activeWorker = ""
	},
}

var sessionResumeCmd = &cobra.Command{
	Use: "resume",
	Run: func(cmd *cobra.Command, args []string) {
		// Resume logic would go here
		s := eng.ResumeSession()
		if s == nil {
			fmt.Println("No active session.")
			return
		}

		fmt.Printf("Resumed session for worker: %s at %s\n", s.WorkerID, s.StartTime.Format(time.RFC3339))
	},
}
var sessionStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show active session",
	Run: func(cmd *cobra.Command, args []string) {
		if activeWorker == "" {
			fmt.Println("No active session.")
			return
		}
		fmt.Printf("Active worker: %s\n", activeWorker)

	},
}

func init() {
	sessionCmd.AddCommand(sessionStartCmd)
	sessionCmd.AddCommand(sessionEndCmd)
	sessionCmd.AddCommand(sessionStatusCmd)

}
