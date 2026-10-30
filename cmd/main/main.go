package main

import (
	"fmt"
	"os"

	commands "github.com/Azure/run-command-handler-linux/internal/cmds"
	"github.com/Azure/run-command-handler-linux/internal/commandProcessor"
	"github.com/Azure/run-command-handler-linux/internal/types"
	"github.com/Azure/run-command-handler-linux/pkg/errorutil"
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
	err := commandProcessor.ProcessHandlerCommand(cmd)

	// If any error is returned, then exit with provided fail exit code (if any)
	if err != nil {
		os.Exit(cmd.FailExitCode)
	}
}

// parseCmd parses the subcommand. If it is invalid, it prints the usage string
// and a clarified error message and exits with code 2.
func parseCmd(args []string) types.Cmd {
	cmd, err := parseCommand(args)
	if err != nil {
		printUsage(args)
		fmt.Println(err)
		os.Exit(2)
	}
	return cmd
}

func parseCommand(args []string) (types.Cmd, error) {
	if len(args) != 2 {
		return types.Cmd{}, errorutil.WithCode(errorutil.CustomerInput_invalidCommandLineArguments, fmt.Errorf("Incorrect usage."))
	}
	op := args[1]
	cmd, ok := commands.Cmds[op]

	// If no command was found, then exit
	if !ok {
		return types.Cmd{}, errorutil.WithCode(errorutil.CustomerInput_unknownCommandLineOperation, fmt.Errorf("Incorrect command: %q", op))
	}

	return cmd, nil
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
