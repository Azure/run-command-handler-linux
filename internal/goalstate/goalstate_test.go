package goalstate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	commands "github.com/Azure/run-command-handler-linux/internal/cmds"
	"github.com/Azure/run-command-handler-linux/internal/constants"
	"github.com/Azure/run-command-handler-linux/internal/handlersettings"
	"github.com/Azure/run-command-handler-linux/internal/observer"
	"github.com/Azure/run-command-handler-linux/internal/settings"
	"github.com/Azure/run-command-handler-linux/internal/status"
	"github.com/Azure/run-command-handler-linux/internal/types"
	"github.com/Azure/run-command-handler-linux/pkg/statusreporter"
	"github.com/go-kit/kit/log"
	"github.com/stretchr/testify/require"
)

type recordingStatusObserver struct {
	events []types.StatusEventArgs
}

func (o *recordingStatusObserver) OnNotify(event types.StatusEventArgs) error {
	o.events = append(o.events, event)
	return nil
}

func (o *recordingStatusObserver) OnDemandNotify() error {
	return nil
}

func Test_HandleImmediateGoalState_preservesHandlerName(t *testing.T) {
	for _, handler := range []string{constants.RunCommandExtensionName, constants.RunCommandTestExtensionName} {
		t.Run(handler, func(t *testing.T) {
			root := t.TempDir()
			env := types.HandlerEnvironment{}
			env.HandlerEnvironment.ConfigFolder = root
			data, err := json.Marshal([]types.HandlerEnvironment{env})
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(filepath.Join(root, handlersettings.HandlerEnvFileName), data, 0600))
			oldExecutable := os.Args[0]
			os.Args[0] = filepath.Join(root, "handler")
			t.Cleanup(func() { os.Args[0] = oldExecutable })
			oldCmd := commands.Cmds[enableCommand]
			cmd := oldCmd
			cmd.Functions.Pre = nil
			cmd.Functions.Invoke = func(_ *log.Context, _ types.HandlerEnvironment, _ *types.RunCommandInstanceView, _ types.RCMetadata, _ types.Cmd) (string, string, error, int) {
				return "command output", "", nil, 0
			}
			commands.Cmds[enableCommand] = cmd
			t.Cleanup(func() { commands.Cmds[enableCommand] = oldCmd })
			recorder := &recordingStatusObserver{}
			notifier := &observer.Notifier{}
			notifier.Register(recorder)
			name, state, seq := "immediate-inline", "enabled", 1
			exitCode, err := HandleImmediateGoalState(log.NewContext(log.NewNopLogger()), handler, settings.SettingsCommon{
				ExtensionName: &name, ExtensionState: &state, SeqNo: &seq,
			}, notifier)
			require.NoError(t, err)
			require.Equal(t, constants.ExitCode_Okay, exitCode)
			require.Len(t, recorder.events, 2)
			for _, event := range recorder.events {
				require.Equal(t, handler, event.StatusKey.HandlerName)
				require.Equal(t, name, event.StatusKey.ExtensionName)
				require.Equal(t, seq, event.StatusKey.SeqNumber)
			}
			require.Equal(t, types.StatusTransitioning, recorder.events[0].TopLevelStatus.Status.Status)
			require.Equal(t, types.StatusSuccess, recorder.events[1].TopLevelStatus.Status.Status)
			require.Contains(t, recorder.events[1].TopLevelStatus.Status.FormattedMessage.Message, "command output")
		})
	}
}

func Test_ReportFinalStatus_preservesHandlerName(t *testing.T) {
	for _, handler := range []string{constants.RunCommandExtensionName, constants.RunCommandTestExtensionName} {
		for _, statusType := range []types.StatusType{types.StatusError, types.StatusSkipped} {
			t.Run(handler+"/"+string(statusType), func(t *testing.T) {
				recorder := &recordingStatusObserver{}
				notifier := &observer.Notifier{}
				notifier.Register(recorder)
				key := types.GoalStateKey{
					HandlerName: handler, ExtensionName: "immediate-inline", SeqNumber: 1, RuntimeSettingsState: "enabled",
				}
				require.NoError(t, ReportFinalStatusForImmediateGoalState(log.NewContext(log.NewNopLogger()), notifier,
					key, statusType, &types.RunCommandInstanceView{ExecutionState: types.Failed}))
				require.Len(t, recorder.events, 1)
				require.Equal(t, key, recorder.events[0].StatusKey)
				require.Equal(t, statusType, recorder.events[0].TopLevelStatus.Status.Status)
			})
		}
	}
}

func Test_handleSkippedImmediateGoalState_NotifyObserver(t *testing.T) {
	ctx := log.NewContext(log.NewSyncLogger(log.NewLogfmtLogger(os.Stdout))).With("time", log.DefaultTimestamp)

	// Create a temporary directory for the test
	tempDir, err := os.MkdirTemp("", "")
	defer os.RemoveAll(tempDir) // Clean up the temp directory after the test
	require.NoError(t, err, "Failed to create temp directory")

	err = os.Mkdir(tempDir+"/status", 0755)
	require.NoError(t, err, "Failed to create status directory")

	os.Setenv(constants.ExtensionPathEnvName, tempDir)

	ctx.Log("msg", "Creating status observer")
	obs := &status.StatusObserver{}
	obs.Initialize(ctx)
	obs.Reporter = statusreporter.TestGuestInformationClient{Endpoint: "localhost:3000/upload"}
	notifier := &observer.Notifier{}

	ctx.Log("msg", "Registering observer")
	notifier.Register(obs)

	goalStateKey := types.GoalStateKey{
		HandlerName:          constants.RunCommandTestExtensionName,
		ExtensionName:        "test",
		SeqNumber:            1,
		RuntimeSettingsState: "enabled",
	}

	errorMsg := "Test error message"
	instView := types.RunCommandInstanceView{
		ExecutionState:   types.Failed,
		ExecutionMessage: "Execution failed",
		ExitCode:         constants.ExitCode_SkippedImmediateGoalState,
		Output:           "",
		Error:            errorMsg,
		StartTime:        time.Now().UTC().Format(time.RFC3339),
		EndTime:          time.Now().UTC().Format(time.RFC3339),
	}

	err = ReportFinalStatusForImmediateGoalState(ctx, notifier, goalStateKey, types.StatusSkipped, &instView)
	require.Nil(t, err, "HandleSkippedImmediateGoalState should not return an error")

	ctx.Log("msg", "Unregistering observer")
	notifier.Unregister()

	ctx.Log("msg", "Check that the status item was received by the observer")
	_, ok := obs.GetStatusForKey(goalStateKey)
	require.False(t, ok, "Status item should not be found because it is in a terminal state and saved locally")

	// Get the status item from the local file
	items, err := status.GetGoalStatesInTerminalStatus(ctx)
	require.Nil(t, err, "GetGoalStatesInTerminalStatus should not return an error")
	require.Len(t, items, 1, "There should be one status item in the local file")

	latestStatus := items[0]
	require.Equal(t, goalStateKey.HandlerName, latestStatus.HandlerName)
	require.Equal(t, "Enable", latestStatus.Status.Operation, "Operation should be equal")
	require.Equal(t, types.StatusSkipped, latestStatus.Status.Status, "Status should be equal")

	json, err := json.Marshal(instView)
	require.Nil(t, err, "Marshal should not return an error")
	require.Equal(t, string(json), latestStatus.Status.FormattedMessage.Message, "Message should be equal")
}
