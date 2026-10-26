package commandProcessor

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"strconv"
	"testing"

	"github.com/Azure/run-command-handler-linux/internal/constants"
	"github.com/Azure/run-command-handler-linux/internal/handlersettings"
	"github.com/Azure/run-command-handler-linux/internal/status"
	"github.com/Azure/run-command-handler-linux/internal/types"
	"github.com/Azure/run-command-handler-linux/pkg/errorutil"
	"github.com/go-kit/kit/log"
	"github.com/stretchr/testify/require"
)

func mockProcessor(t *testing.T) {
	t.Helper()
	read, write, marshal, remove := readHandlerSettings, writeSettingsFile, marshalSettings, removeRuntimePolicyFile
	t.Cleanup(func() {
		readHandlerSettings, writeSettingsFile, marshalSettings, removeRuntimePolicyFile = read, write, marshal, remove
	})
	removeRuntimePolicyFile = func(string) error { return nil }
}

func TestCommandFailurePreservesClarificationAndDeploymentBehavior(t *testing.T) {
	for _, treatAsFailure := range []bool{false, true} {
		for _, settingsFailure := range []bool{false, true} {
			t.Run(strconv.FormatBool(treatAsFailure)+strconv.FormatBool(settingsFailure), func(t *testing.T) {
				mockProcessor(t)
				cause := errorutil.WithCode(errorutil.CommandExecution_failureExitCode, errors.New("original script error"))
				readHandlerSettings = func(string, string, int, *log.Context) (handlersettings.HandlerSettings, error) {
					cfg := handlersettings.HandlerSettings{}
					cfg.PublicSettings.TreatFailureAsDeploymentFailure = treatAsFailure
					if settingsFailure {
						return cfg, errors.New("settings read failed")
					}
					return cfg, nil
				}
				var states []types.StatusType
				var reports []types.StatusReport
				var reported error
				cmd := types.CmdEnableTemplate.InitializeFunctions(types.CmdFunctions{
					Invoke: func(*log.Context, types.HandlerEnvironment, *types.RunCommandInstanceView, types.RCMetadata, types.Cmd) (string, string, error, int) {
						return "stdout", "stderr", cause, 12
					},
					ReportStatus: func(ctx *log.Context, _ types.HandlerEnvironment, m types.RCMetadata, state types.StatusType, cmd types.Cmd, message string) error {
						states = append(states, state)
						reported = m.ClarificationError
						reports = append(reports, processorStatusReport(t, ctx, m, state, cmd, message))
						return nil
					},
				})
				err := ProcessHandlerCommandWithDetails(log.NewContext(log.NewNopLogger()), cmd, types.HandlerEnvironment{}, "command", 1, "download", "/mock")
				require.ErrorIs(t, err, cause)
				code, ok := errorutil.Code(err)
				require.True(t, ok)
				require.Equal(t, errorutil.CommandExecution_failureExitCode, code)
				require.Equal(t, cause, reported)
				expected := types.StatusSuccess
				if treatAsFailure && !settingsFailure {
					expected = types.StatusError
				}
				require.Equal(t, []types.StatusType{types.StatusTransitioning, expected}, states)
				require.Len(t, reports, 2)
				require.Empty(t, reports[0][0].Status.Substatuses, "transitioning status must not contain a failure")
				requireProcessorStatusCode(t, reports[1], errorutil.CommandExecution_failureExitCode)
			})
		}
	}
}

func TestProcessorClarificationErrors(t *testing.T) {
	for _, code := range []int{errorutil.Internal_unclassifiedCommandFailure, errorutil.Internal_invalidSequenceEnvironmentVariable, errorutil.Internal_failedToMarshalLocalSettings, errorutil.Os_FailedToWriteLocalSettings, errorutil.Os_FailedToDeleteRuntimePolicy} {
		t.Run(strconv.Itoa(code), func(t *testing.T) {
			mockProcessor(t)
			failure := errors.New("injected failure")
			ctx := log.NewContext(log.NewNopLogger())
			var err error
			switch code {
			case errorutil.Internal_unclassifiedCommandFailure:
				readHandlerSettings = func(string, string, int, *log.Context) (handlersettings.HandlerSettings, error) {
					return handlersettings.HandlerSettings{}, nil
				}
				cmd := types.CmdEnableTemplate.InitializeFunctions(types.CmdFunctions{
					Invoke: func(*log.Context, types.HandlerEnvironment, *types.RunCommandInstanceView, types.RCMetadata, types.Cmd) (string, string, error, int) {
						return "", "", failure, 1
					},
					ReportStatus: func(ctx *log.Context, _ types.HandlerEnvironment, metadata types.RCMetadata, state types.StatusType, cmd types.Cmd, message string) error {
						report := processorStatusReport(t, ctx, metadata, state, cmd, message)
						if state != types.StatusTransitioning {
							requireProcessorStatusCode(t, report, errorutil.Internal_unclassifiedCommandFailure)
						}
						return nil
					},
				})
				err = ProcessHandlerCommandWithDetails(ctx, cmd, types.HandlerEnvironment{}, "command", 1, "download", "/mock")
			case errorutil.Internal_invalidSequenceEnvironmentVariable:
				t.Setenv(constants.ConfigSequenceNumberEnvName, "invalid")
				_, err = getSeqNum(&ctx, types.HandlerEnvironment{}, "command")
			case errorutil.Internal_failedToMarshalLocalSettings:
				marshalSettings = func(interface{}) ([]byte, error) { return nil, failure }
				err = storeConfigSettingsFileForLocalExecution(ctx, handlersettings.HandlerSettingsFile{}, types.HandlerEnvironment{}, "command", 1)
			case errorutil.Os_FailedToWriteLocalSettings:
				writeSettingsFile = func(string, []byte, os.FileMode) error { return failure }
				err = storeConfigSettingsFileForLocalExecution(ctx, handlersettings.HandlerSettingsFile{}, types.HandlerEnvironment{}, "command", 1)
			case errorutil.Os_FailedToDeleteRuntimePolicy:
				var output bytes.Buffer
				ctx = log.NewContext(log.NewLogfmtLogger(&output))
				removeRuntimePolicyFile = func(string) error { return failure }
				deleteRuntimePolicySettingsFile(ctx, types.HandlerEnvironment{})
				require.Contains(t, output.String(), "clarificationCode="+strconv.Itoa(errorutil.Os_FailedToDeleteRuntimePolicy))
				return
			}
			actual, ok := errorutil.Code(err)
			require.True(t, ok)
			require.Equal(t, code, actual)
		})
	}
}

func TestReportingFailureDoesNotReplaceCommandError(t *testing.T) {
	mockProcessor(t)
	cause := errorutil.WithCode(errorutil.CommandExecution_failureExitCode, errors.New("script failed"))
	readHandlerSettings = func(string, string, int, *log.Context) (handlersettings.HandlerSettings, error) {
		return handlersettings.HandlerSettings{}, nil
	}

	calls := 0
	cmd := types.CmdEnableTemplate.InitializeFunctions(types.CmdFunctions{
		Invoke: func(*log.Context, types.HandlerEnvironment, *types.RunCommandInstanceView, types.RCMetadata, types.Cmd) (string, string, error, int) {
			return "", "", cause, 1
		},
		ReportStatus: func(ctx *log.Context, _ types.HandlerEnvironment, metadata types.RCMetadata, state types.StatusType, cmd types.Cmd, message string) error {
			calls++
			if calls == 2 {
				report := processorStatusReport(t, ctx, metadata, state, cmd, message)
				requireProcessorStatusCode(t, report, errorutil.CommandExecution_failureExitCode)
				return errorutil.WithCode(errorutil.Os_FailedToPublishStatusFile, errors.New("status failed"))
			}
			return nil
		},
	})
	err := ProcessHandlerCommandWithDetails(log.NewContext(log.NewNopLogger()), cmd, types.HandlerEnvironment{}, "command", 1, "download", "/mock")
	require.ErrorIs(t, err, cause)
	code, ok := errorutil.Code(err)
	require.True(t, ok)
	require.Equal(t, errorutil.CommandExecution_failureExitCode, code)
}

func requireProcessorCode(t *testing.T, err error, want int) {
	t.Helper()
	code, ok := errorutil.Code(err)
	require.True(t, ok, "expected classified error: %v", err)
	require.Equal(t, want, code)
}

func TestPrecheckClarificationStatus(t *testing.T) {
	for _, tc := range []struct {
		original error
		code     int
	}{
		{errors.New("unclassified precheck failure"), errorutil.Internal_unclassifiedPrecheckFailure},
		{errorutil.WithCode(errorutil.CustomerInput_invalidSettingsJson, errors.New("classified precheck failure")), errorutil.CustomerInput_invalidSettingsJson},
		// Already processed is intentional control flow, not a production failure.
		{types.ErrAlreadyProcessed, 0},
	} {
		original := tc.original
		t.Run(original.Error(), func(t *testing.T) {
			reported := false
			cmd := types.CmdEnableTemplate.InitializeFunctions(types.CmdFunctions{
				Pre: func(*log.Context, types.HandlerEnvironment, types.RCMetadata, types.Cmd) error { return original },
				ReportStatus: func(_ *log.Context, _ types.HandlerEnvironment, metadata types.RCMetadata, state types.StatusType, _ types.Cmd, _ string) error {
					reported = true
					require.Equal(t, types.StatusError, state)
					code, ok := errorutil.Code(metadata.ClarificationError)
					require.True(t, ok)
					require.Equal(t, tc.code, code)
					report := processorStatusReport(t, log.NewContext(log.NewNopLogger()), metadata, state, types.CmdEnableTemplate, "precheck failed")
					requireProcessorStatusCode(t, report, tc.code)
					return nil
				},
			})
			err := executePreSteps(log.NewContext(log.NewNopLogger()), cmd, types.HandlerEnvironment{}, "command", 1, "download")
			require.ErrorIs(t, err, original)
			if tc.code != 0 {
				requireProcessorCode(t, err, tc.code)
			} else {
				_, classified := errorutil.Code(err)
				require.False(t, classified, "already-processed control flow must remain unclassified")
			}
			require.Equal(t, !errors.Is(original, types.ErrAlreadyProcessed), reported)
		})
	}
}

func processorStatusReport(t *testing.T, ctx *log.Context, metadata types.RCMetadata, state types.StatusType, cmd types.Cmd, message string) types.StatusReport {
	t.Helper()
	item, err := status.GetSingleStatusItem(ctx, state, cmd, message, metadata.ExtName, metadata.ClarificationError)
	require.NoError(t, err)
	payload, err := status.MarshalStatusReportIntoJson(types.StatusReport{item}, false)
	require.NoError(t, err)
	var report types.StatusReport
	require.NoError(t, json.Unmarshal(payload, &report))
	require.Len(t, report, 1)
	return report
}

func requireProcessorStatusCode(t *testing.T, report types.StatusReport, want errorutil.ErrorClarificationCode) {
	t.Helper()
	require.Len(t, report, 1)
	require.Len(t, report[0].Status.Substatuses, 1)
	require.Equal(t, "ErrorClarification", report[0].Status.Substatuses[0].Name)
	require.Equal(t, string(types.StatusError), report[0].Status.Substatuses[0].Status)
	require.Equal(t, want, report[0].Status.Substatuses[0].Code)
}
