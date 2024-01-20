package main

import (
	"fmt"
	"os"

	"github.com/Azure/run-command-handler-linux/internal/immediatecmds"
	"github.com/Azure/run-command-handler-linux/internal/runcommandcommon"
)

// Entry Point for ImmediateRunCommand
func main() {
	cmd, ok := immediatecmds.Cmds["runService"]

	// If no command was found, then exit
	if !ok {
		fmt.Printf("missing runService implementation")
		os.Exit(2)
	}

	runcommandcommon.ProcessGoalState(cmd)
}
