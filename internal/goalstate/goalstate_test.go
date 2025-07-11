package goalstate

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/Azure/run-command-handler-linux/internal/constants"
	"github.com/Azure/run-command-handler-linux/internal/observer"
	"github.com/Azure/run-command-handler-linux/internal/status"
	"github.com/Azure/run-command-handler-linux/internal/types"
	"github.com/Azure/run-command-handler-linux/pkg/statusreporter"
	"github.com/go-kit/kit/log"
	"github.com/stretchr/testify/require"
)

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
	require.Equal(t, "Enable", latestStatus.Status.Operation, "Operation should be equal")
	require.Equal(t, types.StatusSkipped, latestStatus.Status.Status, "Status should be equal")

	json, err := json.Marshal(instView)
	require.Nil(t, err, "Marshal should not return an error")
	require.Equal(t, string(json), latestStatus.Status.FormattedMessage.Message, "Message should be equal")
}
