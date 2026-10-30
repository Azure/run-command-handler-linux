package commandProcessor

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"path/filepath"

	"github.com/Azure/azure-extension-platform/pkg/handlerenv"
	"github.com/Azure/azure-extension-platform/pkg/logging"
	"github.com/Azure/run-command-handler-linux/internal/constants"
	"github.com/Azure/run-command-handler-linux/internal/handlersettings"
	"github.com/Azure/run-command-handler-linux/internal/instanceview"
	"github.com/Azure/run-command-handler-linux/internal/types"
	"github.com/Azure/run-command-handler-linux/pkg/errorutil"
	"github.com/Azure/run-command-handler-linux/pkg/seqnumutil"
	"github.com/Azure/run-command-handler-linux/pkg/versionutil"
	"github.com/go-kit/kit/log"
	"github.com/pkg/errors"
)

var (
	handlerEnvironmentGetter func(name, version string) (he *handlerenv.HandlerEnvironment, _ error) = handlerenv.GetHandlerEnvironment
	readHandlerSettings                                                                              = handlersettings.GetHandlerSettings
	writeSettingsFile                                                                                = os.WriteFile
	marshalSettings                                                                                  = json.Marshal
	removeRuntimePolicyFile                                                                          = os.Remove
)

func ProcessImmediateHandlerCommand(cmd types.Cmd, hs handlersettings.HandlerSettingsFile, extensionName string, seqNum int) error {
	ctx := initializeLogger(cmd)
	ctx = ctx.With("extensionName", extensionName)
	ctx.Log("event", "start")
	ctx.Log("message", "processing immediate command")

	hEnv, err := getHandlerEnv(ctx)
	if err != nil {
		return errors.Wrap(err, "could not get handler environment")
	}

	err = executePreSteps(ctx, cmd, hEnv, extensionName, seqNum, constants.ImmediateDownloadFolder)
	if err != nil {
		return errors.Wrap(err, "failed on pre steps")
	}

	err = storeConfigSettingsFileForLocalExecution(ctx, hs, hEnv, extensionName, seqNum)
	if err != nil {
		return errors.Wrap(err, "failed when trying to store handler settings locally")
	}

	// Store handler settings locally before moving forward...
	return ProcessHandlerCommandWithDetails(ctx, cmd, hEnv, extensionName, seqNum, constants.ImmediateDownloadFolder, constants.DataDir)
}

func ProcessHandlerCommand(cmd types.Cmd) error {
	ctx := initializeLogger(cmd)
	ctx.Log("event", "start")
	ctx.Log("message", "processing command")

	hEnv, extensionName, seqNum, err := getRequiredInitialVariables(ctx)
	if err != nil {
		return errors.Wrap(err, "could not get initial required variables")
	}
	ctx = ctx.With("extensionName", extensionName)

	err = executePreSteps(ctx, cmd, hEnv, extensionName, seqNum, constants.DownloadFolder)
	if err != nil {
		return errors.Wrap(err, "failed on pre steps")
	}

	return ProcessHandlerCommandWithDetails(ctx, cmd, hEnv, extensionName, seqNum, constants.DownloadFolder, constants.DataDir)
}

func ProcessHandlerCommandWithDetails(ctx *log.Context, cmd types.Cmd, hEnv types.HandlerEnvironment, extensionName string, seqNum int, downloadFolder string, dataDir string) error {
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

	metadata := types.NewRCMetadata(extensionName, seqNum, downloadFolder, dataDir)
	if err := instanceview.ReportInstanceView(ctx, hEnv, metadata, types.StatusTransitioning, cmd, &instView); err != nil {
		ctx.Log("event", "failed to report initial instance view", "error", err)
	}

	// execute the subcommand
	stdout, stderr, cmdInvokeError, exitCode := cmd.Functions.Invoke(ctx, hEnv, &instView, metadata, cmd)

	// after the subcommand, delete the policy file if it exists, regardless of the execution outcome
	deleteRuntimePolicySettingsFile(ctx, hEnv)

	instView.Output = stdout
	instView.Error = stderr
	if cmdInvokeError != nil {
		cmdInvokeError = errorutil.WithCode(errorutil.Internal_unclassifiedCommandFailure, cmdInvokeError)
		ctx.Log("event", "failed to handle", "error", cmdInvokeError)
		instView.ExecutionMessage = "Execution failed: " + cmdInvokeError.Error()
		instView.ExecutionState = types.Failed
		instView.EndTime = time.Now().UTC().Format(time.RFC3339)
		instView.ExitCode = exitCode
		instView.ClarificationError = cmdInvokeError
		statusToReport := types.StatusSuccess

		// Fail the extension status and handler invocation only when deployment failure is enabled and the command has a non-zero failure exit code.
		cfg, err := readHandlerSettings(hEnv.HandlerEnvironment.ConfigFolder, extensionName, seqNum, ctx)
		if err == nil && cfg.PublicSettings.TreatFailureAsDeploymentFailure && cmd.FailExitCode != 0 {
			statusToReport = types.StatusError
		}

		if reportErr := instanceview.ReportInstanceView(ctx, hEnv, metadata, statusToReport, cmd, &instView); reportErr != nil {
			ctx.Log("event", "failed to report execution error", "error", reportErr)
		}
		if statusToReport == types.StatusError {
			return errors.Wrapf(cmdInvokeError, "command execution failed")
		}
		return nil
	} else { // No error. Succeeded
		instView.ExecutionMessage = "Execution completed"
		instView.ExecutionState = types.Succeeded
		instView.EndTime = time.Now().UTC().Format(time.RFC3339)
		instView.ExitCode = constants.ExitCode_Okay
	}

	if err := instanceview.ReportInstanceView(ctx, hEnv, metadata, types.StatusSuccess, cmd, &instView); err != nil {
		return errors.Wrap(err, "failed to report final instance view")
	}
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

	extensionName = GetExtensionName(ctx)
	seqNum, err = getSeqNum(&ctx, hEnv, extensionName)
	if err != nil {
		return hEnv, extensionName, seqNum, errors.Wrap(err, "failed to get seqNum")
	}

	return hEnv, extensionName, seqNum, nil
}

func executePreSteps(ctx *log.Context, cmd types.Cmd, hEnv types.HandlerEnvironment, extensionName string, seqNum int, downloadFolder string) error {
	// check sub-command preconditions, if any, before executing
	if cmd.Functions.Pre != nil {
		ctx.Log("event", "pre-check")
		metadata := types.NewRCMetadata(extensionName, seqNum, downloadFolder, constants.DataDir)
		if err := cmd.Functions.Pre(ctx, hEnv, metadata, cmd); err != nil {
			ctx.Log("event", "pre-check failed", "error", err)
			if !errors.Is(err, types.ErrAlreadyProcessed) {
				err = errorutil.WithCode(errorutil.Internal_unclassifiedPrecheckFailure, err)
				view := types.RunCommandInstanceView{
					ExecutionState:     types.Failed,
					ExecutionMessage:   "Pre-check failed: " + err.Error(),
					ExitCode:           cmd.FailExitCode,
					EndTime:            time.Now().UTC().Format(time.RFC3339),
					ClarificationError: err,
				}
				if reportErr := instanceview.ReportInstanceView(ctx, hEnv, metadata, types.StatusError, cmd, &view); reportErr != nil {
					ctx.Log("event", "failed to report pre-check error", "error", reportErr)
				}
			}
			return errors.Wrapf(err, "pre-check step failed")
		}
	}

	return nil
}

func initializeLogger(cmd types.Cmd) *log.Context {
	logging.New(nil)
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

func GetExtensionName(ctx *log.Context) string {
	extensionName := os.Getenv(constants.ConfigExtensionNameEnvName)
	ctx.Log("extensionName", extensionName)
	return extensionName
}

func getSeqNum(ctx **log.Context, hEnv types.HandlerEnvironment, extensionName string) (int, error) {
	// Multiconfig support: Agent should set env variables for the extension name and sequence number
	seqNum := -1
	var err error = nil
	seqNumVariable := os.Getenv(constants.ConfigSequenceNumberEnvName)
	if seqNumVariable != "" {
		seqNum, err = strconv.Atoi(seqNumVariable)
		if err != nil {
			msg := fmt.Sprintf("failed to parse env variable ConfigSequenceNumber: %v", seqNumVariable)
			(*ctx).Log("message", msg, "error", err)
			return seqNum, errorutil.WithCode(errorutil.Internal_invalidSequenceEnvironmentVariable, errors.Wrap(err, msg))
		}
	}

	// Read the seqNum from latest config file in case VMAgent did not set it as env variable
	if seqNum == -1 {
		seqNum, err = seqnumutil.FindSequenceNumberFromConfig(hEnv.HandlerEnvironment.ConfigFolder, constants.ConfigFileExtension, extensionName)
		if err != nil {
			(*ctx).Log("FindSequenceNumberFromConfig", "failed to find sequence number from config folder.", "error", err)
			return seqNum, err
		} else {
			(*ctx).Log("FindSequenceNumberFromConfig", fmt.Sprintf("Sequence number determined from config folder: %d", seqNum))
		}
	}

	(*ctx) = (*ctx).With("seq", seqNum)
	(*ctx).Log("seqNum", seqNum)
	return seqNum, nil
}

func storeConfigSettingsFileForLocalExecution(ctx *log.Context, hs handlersettings.HandlerSettingsFile, hEnv types.HandlerEnvironment, extensionName string, seqNum int) error {
	configFolder := hEnv.HandlerEnvironment.ConfigFolder
	configFilePath := handlersettings.GetConfigFilePath(configFolder, seqNum, extensionName)
	ctx.Log("message", fmt.Sprintf("saving handler settings in file: %v", configFilePath))

	content, err := marshalSettings(hs)
	if err != nil {
		return errorutil.WithCode(errorutil.Internal_failedToMarshalLocalSettings, errors.Wrap(err, "could not marshal handler settings file"))
	}

	err = writeSettingsFile(configFilePath, content, 0644)
	if err != nil {
		return errorutil.WithCode(errorutil.Os_FailedToWriteLocalSettings, errors.Wrap(err, "could not store handler settings file locally to run the command"))
	}

	return nil
}

func deleteRuntimePolicySettingsFile(ctx *log.Context, hEnv types.HandlerEnvironment) {
	policyFilePath := filepath.Join(hEnv.HandlerEnvironment.ConfigFolder, constants.PolicyFileName)
	ctx.Log("message", "removing runtime policy settings file if it exists")
	err := removeRuntimePolicyFile(policyFilePath)
	if err != nil && !os.IsNotExist(err) {
		ctx.Log("warning", "failed to remove runtime policy settings file", "error", errorutil.WithCode(errorutil.Os_FailedToDeleteRuntimePolicy, err), "clarificationCode", errorutil.Os_FailedToDeleteRuntimePolicy)
	}
}
