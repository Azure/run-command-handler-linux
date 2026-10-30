package goalstate

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/Azure/run-command-handler-linux/internal/commandProcessor"
	"github.com/Azure/run-command-handler-linux/internal/handlersettings"
	"github.com/Azure/run-command-handler-linux/internal/hostgacommunicator"
	"github.com/Azure/run-command-handler-linux/internal/observer"
	"github.com/Azure/run-command-handler-linux/internal/settings"
	"github.com/Azure/run-command-handler-linux/internal/types"
	"github.com/Azure/run-command-handler-linux/pkg/errorutil"
	"github.com/go-kit/kit/log"
	"github.com/stretchr/testify/require"
)

type failingCommunicator struct{ err error }

func (c failingCommunicator) GetImmediateVMSettings(*log.Context, string) (*hostgacommunicator.ResponseData, error) {
	return nil, c.err
}

func TestGoalStateClarifications(t *testing.T) {
	oldExecute, oldBuild, oldTimeout := executeImmediateCommand, buildStatusItem, goalStateTimeout
	t.Cleanup(func() { executeImmediateCommand, buildStatusItem, goalStateTimeout = oldExecute, oldBuild, oldTimeout })
	ctx := log.NewContext(log.NewLogfmtLogger(io.Discard))
	cause := errors.New("goal state failure")
	assertCode := func(err error, want int) {
		t.Helper()
		code, ok := errorutil.Code(err)
		require.True(t, ok, "%v", err)
		require.Equal(t, want, code)
	}
	n := &observer.Notifier{}
	key := types.GoalStateKey{RuntimeSettingsState: "enabled"}
	view := &types.RunCommandInstanceView{}
	assertCode(ReportFinalStatusForImmediateGoalState(ctx, nil, key, types.StatusError, view), errorutil.Internal_nilGoalStateNotifier)
	assertCode(ReportFinalStatusForImmediateGoalState(ctx, n, key, types.StatusError, nil), errorutil.Internal_nilGoalStateInstanceView)
	invalidKey := types.GoalStateKey{RuntimeSettingsState: "invalid"}
	assertCode(ReportFinalStatusForImmediateGoalState(ctx, n, invalidKey, types.StatusError, view), errorutil.Internal_unknownGoalStateCommand)
	// errorutil.Internal_failedToMarshalGoalStateInstanceView cannot fail with today's instance view: all serialized fields have
	// JSON-supported scalar types, and ClarificationError is excluded from JSON.
	buildStatusItem = func(*log.Context, types.StatusType, types.Cmd, string, string, ...error) (types.StatusItem, error) {
		return types.StatusItem{}, cause
	}
	err := ReportFinalStatusForImmediateGoalState(ctx, n, key, types.StatusError, view)
	assertCode(err, errorutil.Internal_failedToCreateGoalStateStatus)
	require.ErrorIs(t, err, cause)
	name, state, seq := "test", "enabled", 1
	setting := settings.SettingsCommon{ExtensionName: &name, ExtensionState: &state, SeqNo: &seq}
	runAsync := func(s settings.SettingsCommon, n *observer.Notifier) error {
		t.Helper()
		done, failed := make(chan bool, 1), make(chan error, 1)
		startAsync(ctx, s, n, done, failed)
		select {
		case err := <-failed:
			return err
		default:
			t.Fatal("expected an asynchronous failure")
			return nil
		}
	}
	assertCode(runAsync(setting, nil), errorutil.Internal_nilImmediateCommandNotifier)
	assertCode(runAsync(settings.SettingsCommon{}, n), errorutil.Internal_incompleteImmediateCommandGoalState)
	invalidState := "invalid"
	invalidSetting := setting
	invalidSetting.ExtensionState = &invalidState
	assertCode(runAsync(invalidSetting, n), errorutil.Internal_unknownImmediateCommand)
	executeImmediateCommand = func(cmd types.Cmd, _ handlersettings.HandlerSettingsFile, _ string, _ int) error {
		return cmd.Functions.ReportStatus(ctx, types.HandlerEnvironment{}, types.RCMetadata{}, types.StatusError, cmd, "{}")
	}
	assertCode(runAsync(setting, n), errorutil.Internal_failedToCreateImmediateCommandStatus)
	executeImmediateCommand = func(cmd types.Cmd, _ handlersettings.HandlerSettingsFile, _ string, _ int) error {
		return cmd.Functions.ReportStatus(ctx, types.HandlerEnvironment{}, types.RCMetadata{}, types.StatusError, cmd, "invalid json")
	}
	assertCode(runAsync(setting, n), errorutil.Internal_failedToParseImmediateInstanceView)
	executeImmediateCommand = func(types.Cmd, handlersettings.HandlerSettingsFile, string, int) error { return cause }
	assertCode(runAsync(setting, n), errorutil.Internal_failedToProcessImmediateCommand)
	_, _, err = GetImmediateRunCommandGoalStates(ctx, nil, "etag")
	assertCode(err, errorutil.Internal_nilGoalStateCommunicator)
	_, etag, err := GetImmediateRunCommandGoalStates(ctx, failingCommunicator{cause}, "etag")
	assertCode(err, errorutil.HostGA_failedToRetrieveVmSettings)
	require.Equal(t, "etag", etag)
	require.ErrorIs(t, err, cause)
	buildStatusItem = oldBuild
	release, finished := make(chan struct{}), make(chan struct{})
	executeImmediateCommand = func(types.Cmd, handlersettings.HandlerSettingsFile, string, int) error {
		<-release
		close(finished)
		return nil
	}
	goalStateTimeout = time.Nanosecond
	_, err = HandleImmediateGoalState(ctx, setting, n)
	close(release)
	<-finished
	assertCode(err, errorutil.CommandExecution_immediateGoalStateTimedOut)
}

func TestGoalStateStatusCarriesClarification(t *testing.T) {
	oldExecute, oldBuild := executeImmediateCommand, buildStatusItem
	t.Cleanup(func() { executeImmediateCommand, buildStatusItem = oldExecute, oldBuild })
	ctx := log.NewContext(log.NewLogfmtLogger(io.Discard))
	lower := errorutil.WithCode(errorutil.CustomerInput_invalidSettingsJson, errors.New("customer error"))
	calls := 0
	buildStatusItem = func(ctx *log.Context, state types.StatusType, cmd types.Cmd, message string, name string, clarifications ...error) (types.StatusItem, error) {
		require.Equal(t, []error{lower}, clarifications)
		calls++
		return oldBuild(ctx, state, cmd, message, name, clarifications...)
	}
	n := &observer.Notifier{}
	recorded := &goalStateStatusRecorder{}
	n.Register(recorded)
	require.NoError(t, ReportFinalStatusForImmediateGoalState(ctx, n, types.GoalStateKey{RuntimeSettingsState: "enabled"}, types.StatusError, &types.RunCommandInstanceView{ClarificationError: lower}))
	executeImmediateCommand = func(cmd types.Cmd, _ handlersettings.HandlerSettingsFile, _ string, _ int) error {
		return cmd.Functions.ReportStatus(ctx, types.HandlerEnvironment{}, types.RCMetadata{ClarificationError: lower}, types.StatusError, cmd, "{}")
	}
	name, state, seq := "test", "enabled", 1
	done, failed := make(chan bool, 1), make(chan error, 1)
	startAsync(ctx, settings.SettingsCommon{ExtensionName: &name, ExtensionState: &state, SeqNo: &seq}, n, done, failed)
	require.Len(t, failed, 1)
	var executionErr *ImmediateCommandExecutionError
	require.ErrorAs(t, <-failed, &executionErr)
	require.True(t, executionErr.Reported)
	require.ErrorIs(t, executionErr, lower)
	require.Equal(t, 2, calls)
	require.Len(t, recorded.items, 2)
	for _, item := range recorded.items {
		require.Equal(t, types.StatusError, item.Status.Status)
		requireGoalStateStatusCode(t, item.Status, errorutil.CustomerInput_invalidSettingsJson)
	}
}

func TestImmediateScriptFailureRetainsExecutionResult(t *testing.T) {
	for _, deploymentFailure := range []bool{false, true} {
		for _, reportingFailure := range []bool{false, true} {
			t.Run(strconv.FormatBool(deploymentFailure)+"/"+strconv.FormatBool(reportingFailure), func(t *testing.T) {
				oldExecute := executeImmediateCommand
				t.Cleanup(func() { executeImmediateCommand = oldExecute })
				ctx := log.NewContext(log.NewNopLogger())
				name, state, seq := "script", "enabled", 1
				setting := settings.SettingsCommon{
					ExtensionName: &name, ExtensionState: &state, SeqNo: &seq,
					PublicSettings: map[string]interface{}{
						"source":                          map[string]interface{}{"script": "exit 12"},
						"treatFailureAsDeploymentFailure": deploymentFailure,
					},
				}
				env := types.HandlerEnvironment{}
				env.HandlerEnvironment.ConfigFolder = t.TempDir()
				scriptErr := errorutil.WithCode(errorutil.CommandExecution_failureExitCode, errors.New("script exited with code 12"))
				executeImmediateCommand = func(cmd types.Cmd, hs handlersettings.HandlerSettingsFile, extension string, sequence int) error {
					payload, err := json.Marshal(hs)
					if err != nil {
						return err
					}
					if err := os.WriteFile(handlersettings.GetConfigFilePath(env.HandlerEnvironment.ConfigFolder, sequence, extension), payload, 0600); err != nil {
						return err
					}
					cmd.Functions.Invoke = func(*log.Context, types.HandlerEnvironment, *types.RunCommandInstanceView, types.RCMetadata, types.Cmd) (string, string, error, int) {
						return "script output", "script stderr", scriptErr, 12
					}
					return commandProcessor.ProcessHandlerCommandWithDetails(ctx, cmd, env, extension, sequence, "download", "data")
				}
				recorder := &goalStateStatusRecorder{}
				if reportingFailure {
					recorder.terminalError = errors.New("notification failed")
				}
				notifier := &observer.Notifier{}
				notifier.Register(recorder)
				exitCode, err := HandleImmediateGoalState(ctx, setting, notifier)
				require.Equal(t, 12, exitCode)
				require.ErrorIs(t, err, scriptErr)
				var executionErr *ImmediateCommandExecutionError
				require.ErrorAs(t, err, &executionErr)
				require.Equal(t, !reportingFailure, executionErr.Reported)
				require.ErrorIs(t, executionErr.InstanceView.ClarificationError, scriptErr)
				require.Equal(t, "script output", executionErr.InstanceView.Output)
				require.Equal(t, "script stderr", executionErr.InstanceView.Error)
				require.Equal(t, 12, executionErr.InstanceView.ExitCode)
				require.Equal(t, types.ExecutionState(types.Failed), executionErr.InstanceView.ExecutionState)
				require.NotEmpty(t, executionErr.InstanceView.StartTime)
				require.NotEmpty(t, executionErr.InstanceView.EndTime)
				expectedStatus := types.StatusSuccess
				if deploymentFailure {
					expectedStatus = types.StatusError
				}
				require.Equal(t, expectedStatus, executionErr.Status)
				require.Len(t, recorder.items, 2, "only one transitioning and one terminal result should be produced")
				terminal := recorder.items[1].Status
				require.Equal(t, expectedStatus, terminal.Status)
				requireGoalStateStatusCode(t, terminal, errorutil.CommandExecution_failureExitCode)
				code, ok := errorutil.Code(err)
				require.True(t, ok)
				require.Equal(t, errorutil.CommandExecution_failureExitCode, code)
				require.Positive(t, code, "script failure is a customer error")
				payload, marshalErr := executionErr.InstanceView.Marshal()
				require.NoError(t, marshalErr)
				require.JSONEq(t, string(payload), terminal.FormattedMessage.Message)
			})
		}
	}
}

func TestImmediateSuccessfulCommandReturnsSuccess(t *testing.T) {
	oldExecute := executeImmediateCommand
	t.Cleanup(func() { executeImmediateCommand = oldExecute })
	ctx := log.NewContext(log.NewNopLogger())
	name, state, seq := "script", "enabled", 1
	setting := settings.SettingsCommon{ExtensionName: &name, ExtensionState: &state, SeqNo: &seq}
	recorder := &goalStateStatusRecorder{}
	notifier := &observer.Notifier{}
	notifier.Register(recorder)
	executeImmediateCommand = func(cmd types.Cmd, _ handlersettings.HandlerSettingsFile, _ string, _ int) error {
		view := types.RunCommandInstanceView{ExecutionState: types.Succeeded, ExecutionMessage: "Execution completed", ExitCode: 0, Output: "stdout"}
		message, err := view.Marshal()
		if err != nil {
			return err
		}
		return cmd.Functions.ReportStatus(ctx, types.HandlerEnvironment{}, types.NewRCMetadata(name, seq, "download", "data"), types.StatusSuccess, cmd, string(message))
	}
	exitCode, err := HandleImmediateGoalState(ctx, setting, notifier)
	require.NoError(t, err)
	require.Zero(t, exitCode)
	require.Len(t, recorder.items, 1)
	require.Equal(t, types.StatusSuccess, recorder.items[0].Status.Status)
	require.Empty(t, recorder.items[0].Status.Substatuses)
}

type goalStateStatusRecorder struct {
	items         []types.StatusItem
	terminalError error
}

func (r *goalStateStatusRecorder) OnDemandNotify() error { return nil }

func (r *goalStateStatusRecorder) OnNotify(event types.StatusEventArgs) error {
	payload, err := json.Marshal(event.TopLevelStatus)
	if err != nil {
		return err
	}
	var item types.StatusItem
	if err := json.Unmarshal(payload, &item); err != nil {
		return err
	}
	r.items = append(r.items, item)
	if item.Status.Status != types.StatusTransitioning {
		return r.terminalError
	}
	return nil
}

func requireGoalStateStatusCode(t *testing.T, reported types.Status, want errorutil.ErrorClarificationCode) {
	t.Helper()
	require.Len(t, reported.Substatuses, 1)
	require.Equal(t, "ErrorClarification", reported.Substatuses[0].Name)
	require.Equal(t, string(types.StatusError), reported.Substatuses[0].Status)
	require.Equal(t, want, reported.Substatuses[0].Code)
}
