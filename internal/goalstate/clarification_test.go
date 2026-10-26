package goalstate

import (
	"encoding/json"
	"errors"
	"io"
	"testing"
	"time"

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
		return cmd.Functions.ReportStatus(ctx, types.HandlerEnvironment{}, types.RCMetadata{}, types.StatusError, cmd, "message")
	}
	assertCode(runAsync(setting, n), errorutil.Internal_failedToCreateImmediateCommandStatus)
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
		return cmd.Functions.ReportStatus(ctx, types.HandlerEnvironment{}, types.RCMetadata{ClarificationError: lower}, types.StatusError, cmd, "message")
	}
	name, state, seq := "test", "enabled", 1
	done, failed := make(chan bool, 1), make(chan error, 1)
	startAsync(ctx, settings.SettingsCommon{ExtensionName: &name, ExtensionState: &state, SeqNo: &seq}, n, done, failed)
	require.Len(t, failed, 0)
	require.Equal(t, 2, calls)
	require.Len(t, recorded.items, 2)
	for _, item := range recorded.items {
		require.Equal(t, types.StatusError, item.Status.Status)
		requireGoalStateStatusCode(t, item.Status, errorutil.CustomerInput_invalidSettingsJson)
	}
}

type goalStateStatusRecorder struct{ items []types.StatusItem }

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
	return nil
}

func requireGoalStateStatusCode(t *testing.T, reported types.Status, want errorutil.ErrorClarificationCode) {
	t.Helper()
	require.Len(t, reported.Substatuses, 1)
	require.Equal(t, "ErrorClarification", reported.Substatuses[0].Name)
	require.Equal(t, string(types.StatusError), reported.Substatuses[0].Status)
	require.Equal(t, want, reported.Substatuses[0].Code)
}
