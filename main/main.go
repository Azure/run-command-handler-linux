package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-kit/kit/log"
)

var (
	// dataDir is where we store the downloaded files, logs and state for
	// the extension handler
	dataDir = "/var/lib/waagent/run-command-handler"

	// Directory used for copying the Run Command script file to be able to RunAs a different user.
	// It needs to copied because of permission restrictions. RunAsUser does not have permission to execute under /var/lib/waagent and its subdirectories.
	// %s needs to be replaced by '<RunAsUser>' (RunAs username)
	runAsDir = "/home/%s/waagent/run-command-handler-runas"

	// seqNumFile holds the processed highest sequence number to make
	// sure we do not run the command more than once for the same sequence
	// number. Stored under dataDir.
	//seqNumFile = "seqnum"

	// most recent sequence, which was previously traced by seqNumFile. This was
	// incorrect. The correct way is mrseq.  This file is auto-preserved by the agent.
	mostRecentSequence = "mrseq"

	// Filename where active process keeps track of process id and process start time
	pidFilePath = "pidstart"

	// downloadDir is where we store the downloaded files in the "{downloadDir}/{seqnum}/file"
	// format and the logs as "{downloadDir}/{seqnum}/std(out|err)". Stored under dataDir
	// multiconfig support - when extName is set we use {downloadDir}/{extName}/...
	downloadDir = "download"

	// configSequenceNumber environment variable should be set by VMAgent to sequence number
	configSequenceNumber = "ConfigSequenceNumber"

	// configExtensionName environment variable should be set by VMAgent to extension name
	configExtensionName = "ConfigExtensionName"

	configFileExtension = ".settings"

	// General failed exit code when extension provisioning fails due to service errors.
	failedExitCodeGeneral = -1
)

func main() {
	ctx := log.NewContext(log.NewSyncLogger(log.NewLogfmtLogger(
		os.Stdout))).With("time", log.DefaultTimestamp).With("version", VersionString())

	// parse command line arguments
	cmd := parseCmd(os.Args)
	ctx = ctx.With("operation", strings.ToLower(cmd.name))

	// parse extension environment
	hEnv, err := GetHandlerEnv()
	if err != nil {
		ctx.Log("message", "failed to parse handlerenv", "error", err)
		os.Exit(cmd.failExitCode)
	}

	// Multiconfig support: Agent should set env variables for the extension name and sequence number
	seqNum := -1
	seqNumVariable := os.Getenv(configSequenceNumber)
	if seqNumVariable != "" {
		seqNum, err = strconv.Atoi(seqNumVariable)
		if err != nil {
			ctx.Log("message", "failed to parse env variable ConfigSequenceNumber:"+seqNumVariable, "error", err)
			os.Exit(cmd.failExitCode)
		}
	}

	extensionName := os.Getenv(configExtensionName)
	if extensionName != "" {
		ctx = ctx.With("extensionName", extensionName)
		downloadDir = downloadDir + "/" + extensionName
		mostRecentSequence = extensionName + "." + mostRecentSequence
		pidFilePath = extensionName + "." + pidFilePath
	}

	// Read the seqNum from latest config file in case VMAgent did not set it as env variable
	if seqNum == -1 {
		seqNum, err = FindSequenceNumberFromConfig(hEnv.HandlerEnvironment.ConfigFolder, configFileExtension, extensionName)
		if err != nil {
			ctx.Log("FindSequenceNumberFromConfig", "failed to find sequence number from config folder.", "error", err)
		} else {
			ctx.Log("FindSequenceNumberFromConfig", fmt.Sprintf("Sequence number determined from config folder: %d", seqNum))
		}
	}
	ctx = ctx.With("seq", seqNum)

	// check sub-command preconditions, if any, before executing
	ctx.Log("event", "start")
	if cmd.pre != nil {
		ctx.Log("event", "pre-check")
		if err := cmd.pre(ctx, seqNum); err != nil {
			ctx.Log("event", "pre-check failed", "error", err)
			os.Exit(cmd.failExitCode)
		}
	}
	instanceView := RunCommandInstanceView{
		ExecutionState:   Running,
		ExecutionMessage: "Execution in progress",
		ExitCode:         0,
		Output:           "",
		Error:            "",
		StartTime:        time.Now().UTC().Format(time.RFC3339),
		EndTime:          "",
	}

	reportInstanceView(ctx, hEnv, extensionName, seqNum, StatusTransitioning, cmd, &instanceView)

	// execute the subcommand
	stdout, stderr, err := cmd.invoke(ctx, hEnv, &instanceView, extensionName, seqNum)
	if err != nil {
		ctx.Log("event", "failed to handle", "error", err)
		instanceView.ExecutionMessage = "Execution failed: " + err.Error()
		instanceView.EndTime = time.Now().UTC().Format(time.RFC3339)
		instanceView.ExitCode = cmd.failExitCode
		reportInstanceView(ctx, hEnv, extensionName, seqNum, StatusSuccess, cmd, &instanceView)
		os.Exit(cmd.failExitCode)
	} else { // No error. succeeded
		instanceView.ExecutionMessage = "Execution completed"
		instanceView.ExecutionState = Succeeded
		instanceView.EndTime = time.Now().UTC().Format(time.RFC3339)
		instanceView.ExitCode = 0
	}

	instanceView.Output = stdout
	instanceView.Error = stderr
	reportInstanceView(ctx, hEnv, extensionName, seqNum, StatusSuccess, cmd, &instanceView)
	ctx.Log("event", "end")
}

// parseCmd looks at os.Args and parses the subcommand. If it is invalid,
// it prints the usage string and an error message and exits with code 0.
func parseCmd(args []string) cmd {
	if len(os.Args) != 2 {
		printUsage(args)
		fmt.Println("Incorrect usage.")
		os.Exit(2)
	}
	op := os.Args[1]
	cmd, ok := cmds[op]
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
	for k := range cmds {
		fmt.Printf(k)
		if i != len(cmds)-1 {
			fmt.Printf("|")
		}
		i++
	}
	fmt.Println()
	fmt.Println(DetailedVersionString())
}
