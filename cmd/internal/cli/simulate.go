package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"mini-database/engine"
)

var simulateCmd = &cobra.Command{
	Use:   "simulate",
	Short: "Simulate system behaviours",
}

var crashCmd = &cobra.Command{
	Use:   "crash",
	Short: "Simulate engine crash",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Simulating engine crash....")
		eng = nil
		fmt.Println("Engine memory cleared.")
	},
}

var replayCmd = &cobra.Command{
	Use:   "replay",
	Short: "Reload engine from DB",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Replaying from storage ...")
		var err error
		eng, err = engine.NewEngineWithDB("pos.db")
		if err != nil {
			fmt.Println("Replay failed:", err)
			return
		}

		fmt.Println("Replay complete. Engine state restored.")
	},
}

func init() {
	simulateCmd.AddCommand(crashCmd)
	simulateCmd.AddCommand(replayCmd)
}
