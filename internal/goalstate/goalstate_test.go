package goalstate

import (
	"os"
	"testing"

	"github.com/Azure/run-command-handler-linux/internal/observer"
	"github.com/Azure/run-command-handler-linux/internal/status"
	"github.com/Azure/run-command-handler-linux/internal/types"
	"github.com/Azure/run-command-handler-linux/pkg/statusreporter"
	"github.com/go-kit/kit/log"
	"github.com/stretchr/testify/require"
)

func Test_handleSkippedImmediateGoalState_NotifyObserver(t *testing.T) {
	ctx := log.NewContext(log.NewSyncLogger(log.NewLogfmtLogger(os.Stdout))).With("time", log.DefaultTimestamp)

	ctx.Log("msg", "Creating status observer")
	obs := &status.StatusObserver{}
	obs.Initialize(ctx)
	obs.Reporter = statusreporter.TestGuestInformationClient{Endpoint: "localhost:3000/upload"}
	notifier := &observer.Notifier{}

	ctx.Log("msg", "Registering observer")
	notifier.Register(obs)

	goalStateKey := types.GoalStateKey{
		ExtensionName: "test",
		SeqNumber:     1,
	}
	msg := "test message"
	err := HandleSkippedImmediateGoalState(ctx, notifier, goalStateKey, msg)
	require.Nil(t, err, "HandleSkippedImmediateGoalState should not return an error")

	ctx.Log("msg", "Unregistering observer")
	notifier.Unregister()

	ctx.Log("msg", "Check that the status item was received by the observer")
	latestStatus, ok := obs.GetStatusForKey(goalStateKey)
	require.True(t, ok, "Status item should be found")
	require.Equal(t, "Enable", latestStatus.Status.Operation, "Operation should be equal")
	require.Equal(t, types.StatusSkipped, latestStatus.Status.Status, "Status should be equal")
	require.Equal(t, msg, latestStatus.Status.FormattedMessage.Message, "Message should be equal")
}
