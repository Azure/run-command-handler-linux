package immediateruncommand

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/Azure/run-command-handler-linux/internal/constants"
	"github.com/Azure/run-command-handler-linux/internal/goalstate"
	"github.com/Azure/run-command-handler-linux/internal/observer"
	"github.com/Azure/run-command-handler-linux/internal/settings"
	"github.com/Azure/run-command-handler-linux/internal/types"
	"github.com/Azure/run-command-handler-linux/pkg/errorutil"
	"github.com/go-kit/kit/log"
	"github.com/stretchr/testify/require"
)

func TestConcurrencyLimitClarification(t *testing.T) {
	name, state, seq := "test", "enabled", 1
	valid := settings.SettingsCommon{ExtensionName: &name, ExtensionState: &state, SeqNo: &seq}
	err := newConcurrencyLimitError(5, valid)
	code, ok := errorutil.FindClarification(err)
	require.True(t, ok)
	require.Equal(t, errorutil.CommandExecution_concurrentGoalStateLimitExceeded, code)
	require.Contains(t, err.Error(), "Allowed new goal state count: 5")
}

type immediateStatusRecorder struct{ events []types.StatusEventArgs }

func (r *immediateStatusRecorder) OnDemandNotify() error { return nil }
func (r *immediateStatusRecorder) OnNotify(event types.StatusEventArgs) error {
	r.events = append(r.events, event)
	return nil
}

func TestExecutionFailureFallbackPreservesOriginalResult(t *testing.T) {
	for _, statusType := range []types.StatusType{types.StatusSuccess, types.StatusError} {
		for _, reported := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/%t", statusType, reported), func(t *testing.T) {
				recorder := &immediateStatusRecorder{}
				notifier := &observer.Notifier{}
				notifier.Register(recorder)
				scriptErr := errorutil.EnsureClarification(errorutil.CommandExecution_failureExitCode, errors.New("script failed"))
				view := types.RunCommandInstanceView{
					ExecutionState: types.Failed, ExecutionMessage: "Execution failed: script failed",
					ExitCode: 12, Output: "stdout", Error: "stderr",
					StartTime: "original start", EndTime: "original end", ClarificationError: scriptErr,
				}
				executionErr := &goalstate.ImmediateCommandExecutionError{
					InstanceView: view, Status: statusType, Reported: reported, Err: scriptErr,
				}
				key := types.GoalStateKey{ExtensionName: "script", SeqNumber: 1, RuntimeSettingsState: "enabled"}
				err := reportImmediateGoalStateFailure(log.NewContext(log.NewNopLogger()), notifier, key, "fallback start", constants.ExitCode_ImmediateTaskFailed, fmt.Errorf("wrapped: %w", executionErr))
				require.NoError(t, err)
				if reported {
					require.Empty(t, recorder.events, "an already-reported execution must not produce another terminal result")
					return
				}
				require.Len(t, recorder.events, 1)
				require.Equal(t, key, recorder.events[0].StatusKey)
				payload, err := json.Marshal(recorder.events[0].TopLevelStatus)
				require.NoError(t, err)
				var item types.StatusItem
				require.NoError(t, json.Unmarshal(payload, &item))
				require.Equal(t, statusType, item.Status.Status)
				expected, err := view.Marshal()
				require.NoError(t, err)
				require.JSONEq(t, string(expected), item.Status.FormattedMessage.Message)
				require.Len(t, item.Status.Substatuses, 1)
				require.Equal(t, "ErrorClarification", item.Status.Substatuses[0].Name)
				require.Equal(t, string(types.StatusError), item.Status.Substatuses[0].Status)
				require.Equal(t, errorutil.CommandExecution_failureExitCode, item.Status.Substatuses[0].Code)
			})
		}
	}
}

func TestReportingFailureFallbackPreservesSuccessfulScriptResult(t *testing.T) {
	recorder := &immediateStatusRecorder{}
	notifier := &observer.Notifier{}
	notifier.Register(recorder)
	view := types.RunCommandInstanceView{
		ExecutionState: types.Succeeded, ExecutionMessage: "Execution completed",
		ExitCode: 0, Output: "stdout", Error: "stderr",
		StartTime: "original start", EndTime: "original end",
	}
	executionErr := &goalstate.ImmediateCommandExecutionError{
		InstanceView: view, Status: types.StatusSuccess, Err: errors.New("notification failed"),
	}
	key := types.GoalStateKey{ExtensionName: "script", SeqNumber: 1, RuntimeSettingsState: "enabled"}
	err := reportImmediateGoalStateFailure(log.NewContext(log.NewNopLogger()), notifier, key, "fallback start", constants.ExitCode_ImmediateTaskFailed, fmt.Errorf("wrapped: %w", executionErr))
	require.NoError(t, err)
	require.Len(t, recorder.events, 1)
	require.Equal(t, key, recorder.events[0].StatusKey)
	payload, marshalErr := json.Marshal(recorder.events[0].TopLevelStatus)
	require.NoError(t, marshalErr)
	var item types.StatusItem
	require.NoError(t, json.Unmarshal(payload, &item))
	require.Equal(t, types.StatusSuccess, item.Status.Status)
	expected, marshalErr := view.Marshal()
	require.NoError(t, marshalErr)
	require.JSONEq(t, string(expected), item.Status.FormattedMessage.Message)
	require.Empty(t, item.Status.Substatuses)
}

func TestSetupFailureStillReportsFallback(t *testing.T) {
	recorder := &immediateStatusRecorder{}
	notifier := &observer.Notifier{}
	notifier.Register(recorder)
	cause := errorutil.EnsureClarification(errorutil.FailedToProcessImmediateCommand, errors.New("setup failed"))
	key := types.GoalStateKey{ExtensionName: "script", SeqNumber: 1, RuntimeSettingsState: "enabled"}
	require.NoError(t, reportImmediateGoalStateFailure(log.NewContext(log.NewNopLogger()), notifier, key, "start", constants.ExitCode_ImmediateTaskFailed, cause))
	require.Len(t, recorder.events, 1)
	payload, err := json.Marshal(recorder.events[0].TopLevelStatus)
	require.NoError(t, err)
	var item types.StatusItem
	require.NoError(t, json.Unmarshal(payload, &item))
	require.Equal(t, types.StatusError, item.Status.Status)
	var view types.RunCommandInstanceView
	require.NoError(t, json.Unmarshal([]byte(item.Status.FormattedMessage.Message), &view))
	require.Equal(t, constants.ExitCode_ImmediateTaskFailed, view.ExitCode)
	require.Equal(t, "start", view.StartTime)
	require.Equal(t, cause.Error(), view.Error)
	require.Len(t, item.Status.Substatuses, 1)
	require.Equal(t, "ErrorClarification", item.Status.Substatuses[0].Name)
	require.Equal(t, string(types.StatusError), item.Status.Substatuses[0].Status)
	require.Equal(t, errorutil.FailedToProcessImmediateCommand, item.Status.Substatuses[0].Code)
}
