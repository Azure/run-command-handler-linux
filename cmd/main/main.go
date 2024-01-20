package main

import (
	"fmt"
	"os"

	commands "github.com/Azure/run-command-handler-linux/internal/cmds"
	"github.com/Azure/run-command-handler-linux/internal/runcommandcommon"
	"github.com/Azure/run-command-handler-linux/internal/types"
	"github.com/Azure/run-command-handler-linux/pkg/versionutil"
)

// These fields are populated by govvv at compile-time.
var (
	Version   string
	GitCommit string
	BuildDate string
	GitState  string
)

// Entry point for RunCommand
func main() {
	// After starting the program, vars from versionutil.go must be set in order to share those values across the program.
	versionutil.Initialize(Version, GitCommit, BuildDate, GitState)

	// parse command line arguments
	cmd := parseCmd(os.Args)
	runcommandcommon.ProcessGoalState(cmd)
}

// parseCmd looks at os.Args and parses the subcommand. If it is invalid,
// it prints the usage string and an error message and exits with code 0.
func parseCmd(args []string) types.Cmd {
	if len(os.Args) != 2 {
		printUsage(args)
		fmt.Println("Incorrect usage.")
		os.Exit(2)
	}
	op := os.Args[1]
	cmd, ok := commands.Cmds[op]

	// If no command was found, then exit
	if !ok {
		printUsage(args)
		fmt.Printf("Incorrect command: %q\n", op)
		os.Exit(2)
	}

	return cmd
}

// printUsage prints the help string and version of the program to stdout with a
// trailing new line.
func printUsage(args []string) {
	cmds := commands.Cmds
	printCommandsUsage(cmds)
	fmt.Println(versionutil.DetailedVersionString())
}

// printCommandsUsage prints the format needed to launch the executable.
func printCommandsUsage(cmds map[string]types.Cmd) {
	fmt.Printf("Usage: %s ", os.Args[0])
	i, total := 1, len(cmds)
	for k := range cmds {
		fmt.Print(k)
		if i < total {
			fmt.Printf("|")
		}
		i++
	}
	fmt.Println()
}
