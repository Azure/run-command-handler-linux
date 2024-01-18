package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	commands "github.com/Azure/run-command-handler-linux/internal/cmds"
	"github.com/Azure/run-command-handler-linux/internal/constants"
	"github.com/Azure/run-command-handler-linux/internal/handlersettings"
	"github.com/Azure/run-command-handler-linux/internal/instanceview"
	"github.com/Azure/run-command-handler-linux/internal/types"
	seqnum "github.com/Azure/run-command-handler-linux/pkg/seqnumutil"
	"github.com/Azure/run-command-handler-linux/pkg/versionutil"
	"github.com/go-kit/kit/log"
)

// These fields are populated by govvv at compile-time.
var (
	Version   string
	GitCommit string
	BuildDate string
	GitState  string
)

func main() {
	// After starting the program, vars from versionutil.go must be set in order to share those values across the program.
	versionutil.Initialize(Version, GitCommit, BuildDate, GitState)

	ctx := log.NewContext(log.NewSyncLogger(log.NewLogfmtLogger(
		os.Stdout))).With("time", log.DefaultTimestamp).With("version", versionutil.VersionString())

	// parse command line arguments
	cmd := parseCmd(os.Args)
	ctx = ctx.With("operation", strings.ToLower(cmd.Name))

	// parse extension environment
	hEnv, err := handlersettings.GetHandlerEnv()
	if err != nil {
		ctx.Log("message", "failed to parse handlerenv", "error", err)
		os.Exit(cmd.FailExitCode)
	}

	// Multiconfig support: Agent should set env variables for the extension name and sequence number
	seqNum := -1
	seqNumVariable := os.Getenv(constants.ConfigSequenceNumber)
	if seqNumVariable != "" {
		seqNum, err = strconv.Atoi(seqNumVariable)
		if err != nil {
			ctx.Log("message", "failed to parse env variable ConfigSequenceNumber:"+seqNumVariable, "error", err)
			os.Exit(cmd.FailExitCode)
		}
	}

	extensionName := os.Getenv(constants.ConfigExtensionName)
	if extensionName != "" {
		ctx = ctx.With("extensionName", extensionName)
		constants.DownloadDir = constants.DownloadDir + "/" + extensionName
		constants.MostRecentSequence = extensionName + "." + constants.MostRecentSequence
		constants.PidFilePath = extensionName + "." + constants.PidFilePath
	}

	// Read the seqNum from latest config file in case VMAgent did not set it as env variable
	if seqNum == -1 {
		seqNum, err = seqnum.FindSequenceNumberFromConfig(hEnv.HandlerEnvironment.ConfigFolder, constants.ConfigFileExtension, extensionName)
		if err != nil {
			ctx.Log("FindSequenceNumberFromConfig", "failed to find sequence number from config folder.", "error", err)
		} else {
			ctx.Log("FindSequenceNumberFromConfig", fmt.Sprintf("Sequence number determined from config folder: %d", seqNum))
		}
	}
	ctx = ctx.With("seq", seqNum)

	// check sub-command preconditions, if any, before executing
	ctx.Log("event", "start")
	if cmd.Pre != nil {
		ctx.Log("event", "pre-check")
		if err := cmd.Pre(ctx, hEnv, extensionName, seqNum); err != nil {
			ctx.Log("event", "pre-check failed", "error", err)
			os.Exit(cmd.FailExitCode)
		}
	}
	instView := types.RunCommandInstanceView{
		ExecutionState:   types.Running,
		ExecutionMessage: "Execution in progress",
		ExitCode:         0,
		Output:           "",
		Error:            "",
		StartTime:        time.Now().UTC().Format(time.RFC3339),
		EndTime:          "",
	}

	instanceview.ReportInstanceView(ctx, hEnv, extensionName, seqNum, types.StatusTransitioning, cmd, &instView)

	// execute the subcommand
	stdout, stderr, cmdInvokeError, exitCode := cmd.Invoke(ctx, hEnv, &instView, extensionName, seqNum)
	if cmdInvokeError != nil {
		ctx.Log("event", "failed to handle", "error", cmdInvokeError)
		instView.ExecutionMessage = "Execution failed: " + cmdInvokeError.Error()
		instView.ExecutionState = types.Failed
		instView.EndTime = time.Now().UTC().Format(time.RFC3339)
		instView.ExitCode = exitCode
		statusToReport := types.StatusSuccess

		// If TreatFailureAsDeploymentFailure is set to true and the exit code is non-zero, set extension status to error
		cfg, err := handlersettings.GetHandlerSettings(hEnv.HandlerEnvironment.ConfigFolder, extensionName, seqNum, ctx)
		if err == nil && cfg.PublicSettings.TreatFailureAsDeploymentFailure && cmd.FailExitCode != 0 {
			statusToReport = types.StatusError
		}

		instanceview.ReportInstanceView(ctx, hEnv, extensionName, seqNum, statusToReport, cmd, &instView)
		os.Exit(cmd.FailExitCode)
	} else { // No error. succeeded
		instView.ExecutionMessage = "Execution completed"
		instView.ExecutionState = types.Succeeded
		instView.EndTime = time.Now().UTC().Format(time.RFC3339)
		instView.ExitCode = constants.ExitCode_Okay
	}

	instView.Output = stdout
	instView.Error = stderr
	instanceview.ReportInstanceView(ctx, hEnv, extensionName, seqNum, types.StatusSuccess, cmd, &instView)
	ctx.Log("event", "end")
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
	fmt.Printf("Usage: %s ", os.Args[0])
	i := 0
	for k := range commands.Cmds {
		fmt.Print(k)
		if i != len(commands.Cmds)-1 {
			fmt.Printf("|")
		}
		i++
	}
	fmt.Println()
	fmt.Println(versionutil.DetailedVersionString())
}
