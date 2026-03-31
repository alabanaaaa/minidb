package cli

import (
	"fmt"

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
		state, err := loadSessionState()
		if err == nil && state.ActiveWorker != "" {
			fmt.Println("A session is already active. End it first.")
			return
		}

		if err := setActiveWorker(args[0]); err != nil {
			fmt.Println("Failed to start session:", err)
			return
		}

		fmt.Printf("Session started for worker: %s\n", args[0])
	},
}

var sessionEndCmd = &cobra.Command{
	Use:   "end",
	Short: "End active session",
	Run: func(cmd *cobra.Command, args []string) {
		state, err := loadSessionState()
		if err != nil || state.ActiveWorker == "" {
			fmt.Println("No active session.")
			return
		}

		worker := state.ActiveWorker
		if err := clearActiveWorker(); err != nil {
			fmt.Println("Failed to end session:", err)
			return
		}

		fmt.Printf("Session ended for worker: %s\n", worker)
	},
}

var sessionResumeCmd = &cobra.Command{
	Use: "resume",
	Run: func(cmd *cobra.Command, args []string) {
		state, err := loadSessionState()
		if err != nil || state.ActiveWorker == "" {
			fmt.Println("No active session.")
			return
		}

		fmt.Printf("Resumed session for worker: %s\n", state.ActiveWorker)
	},
}
var sessionStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show active session",
	Run: func(cmd *cobra.Command, args []string) {
		state, err := loadSessionState()
		if err != nil || state.ActiveWorker == "" {
			fmt.Println("No active session.")
			return
		}
		fmt.Printf("Active worker: %s\n", state.ActiveWorker)

	},
}

func init() {
	sessionCmd.AddCommand(sessionStartCmd)
	sessionCmd.AddCommand(sessionEndCmd)
	sessionCmd.AddCommand(sessionStatusCmd)

}
