package observer

import (
	"os"
	"testing"

	"github.com/Azure/run-command-handler-linux/internal/status"
	"github.com/Azure/run-command-handler-linux/internal/types"
	"github.com/Azure/run-command-handler-linux/pkg/statusreporter"
	"github.com/go-kit/kit/log"
	"github.com/stretchr/testify/require"
)

func Test_registerObserver(t *testing.T) {
	obs := &status.StatusObserver{}
	n := &Notifier{}
	n.Register(obs)
	require.NotNil(t, n.observer)
}

func Test_unregisterObserver(t *testing.T) {
	obs := &status.StatusObserver{}
	n := &Notifier{}
	n.Register(obs)
	n.Unregister()
	require.Nil(t, n.observer)
}

func Test_notifyObserver(t *testing.T) {
	ctx := log.NewContext(log.NewSyncLogger(log.NewLogfmtLogger(os.Stdout))).With("time", log.DefaultTimestamp)

	ctx.Log("msg", "Creating status observer")
	obs := &status.StatusObserver{}
	obs.Initialize(ctx)
	obs.Reporter = statusreporter.TestGuestInformationClient{Endpoint: "localhost:3000/upload"}
	n := &Notifier{}

	ctx.Log("msg", "Registering observer")
	n.Register(obs)

	nonEmptyStatusItem := getNonEmptyStatusItem()
	statusKey := types.GoalStateKey{
		ExtensionName: "test",
		SeqNumber:     1,
	}
	status := types.StatusEventArgs{
		TopLevelStatus: nonEmptyStatusItem,
		StatusKey:      statusKey,
	}

	ctx.Log("msg", "Notifying observer")
	err := n.Notify(status)
	require.Nil(t, err, "Notify should not return an error")

	ctx.Log("msg", "Unregistering observer")
	n.Unregister()

	ctx.Log("msg", "Check that the status item was received by the observer")
	latestStatus, ok := obs.GetStatusForKey(statusKey)
	require.True(t, ok, "Status item should be found")
	require.Equal(t, nonEmptyStatusItem, latestStatus, "Status item should be the same as the one sent")
}

func Test_notifyObserver_NotRegisteredObserver(t *testing.T) {
	ctx := log.NewContext(log.NewSyncLogger(log.NewLogfmtLogger(os.Stdout))).With("time", log.DefaultTimestamp)
	n := &Notifier{}

	nonEmptyStatusItem := getNonEmptyStatusItem()
	statusKey := types.GoalStateKey{
		ExtensionName: "test",
		SeqNumber:     1,
	}
	status := types.StatusEventArgs{
		TopLevelStatus: nonEmptyStatusItem,
		StatusKey:      statusKey,
	}

	ctx.Log("msg", "Notifying observer")
	err := n.Notify(status)
	require.Nil(t, err, "Notify should not return an error when observer is not registered")
}

func getNonEmptyStatusItem() types.StatusItem {
	return types.StatusItem{
		Version:      2,
		TimestampUTC: "2021-09-01T12:00:00Z",
		Status: types.Status{
			Operation: "TestOperation",
			Status:    "TestStatus",
			FormattedMessage: types.FormattedMessage{
				Message: "Test message",
				Lang:    "en-US",
			},
		},
	}
}
