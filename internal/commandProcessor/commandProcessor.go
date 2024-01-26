package commandProcessor

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Azure/run-command-handler-linux/internal/constants"
	"github.com/Azure/run-command-handler-linux/internal/handlersettings"
	"github.com/Azure/run-command-handler-linux/internal/instanceview"
	"github.com/Azure/run-command-handler-linux/internal/types"
	"github.com/Azure/run-command-handler-linux/pkg/seqnumutil"
	"github.com/Azure/run-command-handler-linux/pkg/versionutil"
	"github.com/go-kit/kit/log"
	"github.com/pkg/errors"
)

func ProcessImmediateHandlerCommand(cmd types.Cmd, hs handlersettings.HandlerSettingsFile, extensionName string, seqNum int) error {
	ctx := initializeLogger(cmd)
	ctx = ctx.With("extensionName", extensionName)
	ctx.Log("event", "start")

	hEnv, err := getHandlerEnv(ctx)
	if err != nil {
		return errors.Wrap(err, "could not get handler environment")
	}

	err = executePreSteps(ctx, cmd, hEnv, extensionName, seqNum, constants.ImmediateDownloadFolder)
	if err != nil {
		return errors.Wrap(err, "failed on pre steps")
	}

	err = storeHandlerSettingsFileForLocalExecution(ctx, hs, hEnv, extensionName, seqNum)
	if err != nil {
		return errors.Wrap(err, "failed when trying to store handler settings locally")
	}

	// Store handler settings locally before moving forward...
	return ProcessHandlerCommandWithDetails(ctx, cmd, hEnv, extensionName, seqNum, constants.ImmediateDownloadFolder)
}

func ProcessHandlerCommand(cmd types.Cmd) error {
	ctx := initializeLogger(cmd)
	ctx.Log("event", "start")

	hEnv, extensionName, seqNum, err := getRequiredInitialVariables(ctx)
	if err != nil {
		return errors.Wrap(err, "could not get initial required variables")
	}
	ctx = ctx.With("extensionName", extensionName)

	err = executePreSteps(ctx, cmd, hEnv, extensionName, seqNum, constants.DownloadFolder)
	if err != nil {
		return errors.Wrap(err, "failed on pre steps")
	}

	return ProcessHandlerCommandWithDetails(ctx, cmd, hEnv, extensionName, seqNum, constants.DownloadFolder)
}

func ProcessHandlerCommandWithDetails(ctx *log.Context, cmd types.Cmd, hEnv types.HandlerEnvironment, extensionName string, seqNum int, downloadFolder string) error {
	ctx.Log("message", fmt.Sprintf("processing command for extensionName: %v and seqNum: %v", extensionName, seqNum))
	instView := types.RunCommandInstanceView{
		ExecutionState:   types.Running,
		ExecutionMessage: "Execution in progress",
		ExitCode:         0,
		Output:           "",
		Error:            "",
		StartTime:        time.Now().UTC().Format(time.RFC3339),
		EndTime:          "",
	}

	metadata := types.NewRCMetadata(extensionName, seqNum, downloadFolder)
	instanceview.ReportInstanceView(ctx, hEnv, metadata, types.StatusTransitioning, cmd, &instView)

	// execute the subcommand
	stdout, stderr, cmdInvokeError, exitCode := cmd.Functions.Invoke(ctx, hEnv, &instView, metadata, cmd)
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

		instanceview.ReportInstanceView(ctx, hEnv, metadata, statusToReport, cmd, &instView)
		return errors.Wrapf(err, "command execution failed")
	} else { // No error. Succeeded
		instView.ExecutionMessage = "Execution completed"
		instView.ExecutionState = types.Succeeded
		instView.EndTime = time.Now().UTC().Format(time.RFC3339)
		instView.ExitCode = constants.ExitCode_Okay
	}

	instView.Output = stdout
	instView.Error = stderr
	instanceview.ReportInstanceView(ctx, hEnv, metadata, types.StatusSuccess, cmd, &instView)
	ctx.Log("event", "end")

	return nil
}

func getRequiredInitialVariables(ctx *log.Context) (types.HandlerEnvironment, string, int, error) {
	var seqNum int
	var extensionName string
	ctx.Log("message", "getting required initial variables")
	hEnv, err := getHandlerEnv(ctx)
	if err != nil {
		return hEnv, extensionName, seqNum, errors.Wrap(err, "failed to parse handlerEnv")
	}

	extensionName = getExtensionName(ctx)
	seqNum, err = getSeqNum(ctx, hEnv, extensionName)
	if err != nil {
		return hEnv, extensionName, seqNum, errors.Wrap(err, "failed to get seqNum")
	}

	return hEnv, extensionName, seqNum, nil
}

func executePreSteps(ctx *log.Context, cmd types.Cmd, hEnv types.HandlerEnvironment, extensionName string, seqNum int, downloadFolder string) error {
	// check sub-command preconditions, if any, before executing
	if cmd.Functions.Pre != nil {
		ctx.Log("event", "pre-check")
		metadata := types.NewRCMetadata(extensionName, seqNum, downloadFolder)
		if err := cmd.Functions.Pre(ctx, hEnv, metadata, cmd); err != nil {
			ctx.Log("event", "pre-check failed", "error", err)
			return errors.Wrapf(err, "pre-check step failed")
		}
	}

	return nil
}

func initializeLogger(cmd types.Cmd) *log.Context {
	ctx := log.NewContext(log.NewSyncLogger(log.NewLogfmtLogger(
		os.Stdout))).With("time", log.DefaultTimestamp).With("version", versionutil.VersionString())
	ctx = ctx.With("operation", strings.ToLower(cmd.Name))
	return ctx
}

func getHandlerEnv(ctx *log.Context) (types.HandlerEnvironment, error) {
	// parse extension handler environment
	hEnv, err := handlersettings.GetHandlerEnv()
	if err != nil {
		ctx.Log("message", "failed to parse handlerEnv", "error", err)
		return hEnv, err
	}
	return hEnv, nil
}

func getExtensionName(ctx *log.Context) string {
	extensionName := os.Getenv(constants.ConfigExtensionNameEnvName)
	ctx.Log("extensionName", extensionName)
	return extensionName
}

func getSeqNum(ctx *log.Context, hEnv types.HandlerEnvironment, extensionName string) (int, error) {
	// Multiconfig support: Agent should set env variables for the extension name and sequence number
	seqNum := -1
	var err error = nil
	seqNumVariable := os.Getenv(constants.ConfigSequenceNumberEnvName)
	if seqNumVariable != "" {
		seqNum, err = strconv.Atoi(seqNumVariable)
		if err != nil {
			msg := fmt.Sprintf("failed to parse env variable ConfigSequenceNumber: %v", seqNumVariable)
			ctx.Log("message", msg, "error", err)
			return seqNum, errors.Wrap(err, msg)
		}
	}

	// Read the seqNum from latest config file in case VMAgent did not set it as env variable
	if seqNum == -1 {
		seqNum, err = seqnumutil.FindSequenceNumberFromConfig(hEnv.HandlerEnvironment.ConfigFolder, constants.ConfigFileExtension, extensionName)
		if err != nil {
			ctx.Log("FindSequenceNumberFromConfig", "failed to find sequence number from config folder.", "error", err)
		} else {
			ctx.Log("FindSequenceNumberFromConfig", fmt.Sprintf("Sequence number determined from config folder: %d", seqNum))
		}
	}

	ctx = ctx.With("seq", seqNum)
	ctx.Log("seqNum", seqNum)
	return seqNum, nil
}

func storeHandlerSettingsFileForLocalExecution(ctx *log.Context, hs handlersettings.HandlerSettingsFile, hEnv types.HandlerEnvironment, extensionName string, seqNum int) error {
	configFolder := hEnv.HandlerEnvironment.ConfigFolder
	configFilePath := handlersettings.GetConfigFilePath(configFolder, seqNum, extensionName)
	ctx.Log("message", fmt.Sprintf("saving handler settings in file: %v", configFilePath))

	content, err := json.Marshal(hs)
	if err != nil {
		return errors.Wrap(err, "could not marshal handler settings file")
	}

	err = os.WriteFile(configFilePath, content, 0644)
	if err != nil {
		return errors.Wrap(err, "could not store handler settings file locally to run the command")
	}

	return nil
}
