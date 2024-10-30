package status

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"

	"github.com/Azure/run-command-handler-linux/internal/types"
	"github.com/Azure/run-command-handler-linux/pkg/statusreporter"
	"github.com/ahmetb/go-httpbin"
	"github.com/go-kit/kit/log"
	"github.com/stretchr/testify/require"
)

func Test_onNotify_success(t *testing.T) {
	ctx := log.NewContext(log.NewSyncLogger(log.NewLogfmtLogger(os.Stdout))).With("time", log.DefaultTimestamp)
	observer := StatusObserver{}
	observer.Initialize(ctx)
	observer.Reporter = statusreporter.TestGuestInformationClient{Endpoint: "localhost:3000/upload"}

	emptyStatusItem := types.StatusItem{}
	observer.goalStateEventMap.Store(types.GoalStateKey{SeqNumber: 1, ExtensionName: "testExtension"}, emptyStatusItem)

	nonEmptyStatusItem := types.StatusItem{
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
	topStatus := types.StatusEventArgs{
		StatusKey:      types.GoalStateKey{SeqNumber: 1, ExtensionName: "testExtension"},
		TopLevelStatus: nonEmptyStatusItem,
	}
	err := observer.OnNotify(topStatus)
	require.Nil(t, err, "OnNotify should not return an error")
	status, ok := observer.GetStatusForKey(types.GoalStateKey{SeqNumber: 1, ExtensionName: "testExtension"})
	require.True(t, ok, "Status should be found")
	require.Equal(t, nonEmptyStatusItem, status, "Status should be the same because it was updated")
}

func Test_onNotify_fails(t *testing.T) {
	ctx := log.NewContext(log.NewSyncLogger(log.NewLogfmtLogger(os.Stdout))).With("time", log.DefaultTimestamp)
	srv := httptest.NewServer(httpbin.GetMux())
	defer srv.Close()

	observer := StatusObserver{}
	observer.Initialize(ctx)
	observer.Reporter = statusreporter.NewGuestInformationServiceClient(srv.URL + "/uploadnotexistent")

	emptyStatusItem := types.StatusItem{}
	observer.goalStateEventMap.Store(types.GoalStateKey{SeqNumber: 1, ExtensionName: "testExtension"}, emptyStatusItem)

	nonEmptyStatusItem := types.StatusItem{
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
	topStatus := types.StatusEventArgs{
		StatusKey:      types.GoalStateKey{SeqNumber: 1, ExtensionName: "testExtension"},
		TopLevelStatus: nonEmptyStatusItem,
	}
	err := observer.OnNotify(topStatus)
	require.NotNil(t, err, "OnNotify should return an error")
	require.Contains(t, err.Error(), strconv.Itoa(http.StatusNotFound))

	status, ok := observer.GetStatusForKey(types.GoalStateKey{SeqNumber: 1, ExtensionName: "testExtension"})
	require.True(t, ok, "Status should be found")
	require.Equal(t, nonEmptyStatusItem, status, "Status should be the same because it was updated even though the report failed")
}

func Test_getImmediateTopLevelStatusToReport_filterNone(t *testing.T) {
	ctx := log.NewContext(log.NewSyncLogger(log.NewLogfmtLogger(os.Stdout))).With("time", log.DefaultTimestamp)
	observer := StatusObserver{}
	observer.Initialize(ctx)

	ctx.Log("message", "Adding non-empty goal states to the event map")
	nonEmptyStatus := types.StatusItem{
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

	goalStateKeys := []types.GoalStateKey{
		{SeqNumber: 1, ExtensionName: "testExtension"},
		{SeqNumber: 2, ExtensionName: "testExtension"},
		{SeqNumber: 3, ExtensionName: "testExtension"},
	}
	for _, goalStateKey := range goalStateKeys {
		observer.goalStateEventMap.Store(goalStateKey, nonEmptyStatus)
	}

	ctx.Log("message", "Getting all goal states from the event map with the latest status that are not empty")
	immediateTopLevelStatus := observer.getImmediateTopLevelStatusToReport()
	require.Equal(t, 1, len(immediateTopLevelStatus.AggregateHandlerImmediateStatus), "Only one handler should be reported in the immediate status")

	for _, handler := range immediateTopLevelStatus.AggregateHandlerImmediateStatus {
		require.Equal(t, "RunCommandHandler", handler.HandlerName, "Handler name should be the same")
		require.Equal(t, len(goalStateKeys), len(handler.AggregateImmediateStatus), "All goal states should be reported")
		for _, immediateStatus := range handler.AggregateImmediateStatus {
			require.Equal(t, nonEmptyStatus, immediateStatus.Status, "Status should be the same")
			require.Equal(t, immediateStatus.TimestampUTC, immediateStatus.Status.TimestampUTC, "Timestamp should be the same")
			require.Contains(t, goalStateKeys, types.GoalStateKey{SeqNumber: immediateStatus.SequenceNumber, ExtensionName: "testExtension"}, "Sequence number should be the same")
		}
	}
}

func Test_getImmediateTopLevelStatusToReport_filterAllWithEmptyStatus(t *testing.T) {
	ctx := log.NewContext(log.NewSyncLogger(log.NewLogfmtLogger(os.Stdout))).With("time", log.DefaultTimestamp)
	observer := StatusObserver{}
	observer.Initialize(ctx)

	ctx.Log("message", "Adding non-empty goal states to the event map")
	emptyStatus := types.StatusItem{}
	goalStateKeys := []types.GoalStateKey{
		{SeqNumber: 1, ExtensionName: "testExtension"},
		{SeqNumber: 2, ExtensionName: "testExtension"},
		{SeqNumber: 3, ExtensionName: "testExtension"},
	}
	for _, goalStateKey := range goalStateKeys {
		observer.goalStateEventMap.Store(goalStateKey, emptyStatus)
	}

	ctx.Log("message", "Getting all goal states from the event map with the latest status that are not empty")
	immediateTopLevelStatus := observer.getImmediateTopLevelStatusToReport()
	require.Equal(t, 1, len(immediateTopLevelStatus.AggregateHandlerImmediateStatus), "Only one handler should be reported in the immediate status")

	for _, handler := range immediateTopLevelStatus.AggregateHandlerImmediateStatus {
		require.Equal(t, "RunCommandHandler", handler.HandlerName, "Handler name should be the same")
		require.Equal(t, 0, len(handler.AggregateImmediateStatus), "No goal states should be reported")
	}
}

func Test_getImmediateTopLevelStatusToReport_filterSomeWithEmptyStatus(t *testing.T) {
	ctx := log.NewContext(log.NewSyncLogger(log.NewLogfmtLogger(os.Stdout))).With("time", log.DefaultTimestamp)
	observer := StatusObserver{}
	observer.Initialize(ctx)

	ctx.Log("message", "Adding non-empty goal states to the event map")
	nonEmptyStatus := types.StatusItem{
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
	emptyStatus := types.StatusItem{}

	nonEmptyGoalStateKeys := []types.GoalStateKey{
		{SeqNumber: 1, ExtensionName: "testExtension"},
		{SeqNumber: 2, ExtensionName: "testExtension"},
		{SeqNumber: 3, ExtensionName: "testExtension"},
	}
	for _, goalStateKey := range nonEmptyGoalStateKeys {
		observer.goalStateEventMap.Store(goalStateKey, nonEmptyStatus)
	}

	emptyGoalStateKeys := []types.GoalStateKey{
		{SeqNumber: 4, ExtensionName: "testExtension"},
		{SeqNumber: 5, ExtensionName: "testExtension"},
		{SeqNumber: 6, ExtensionName: "testExtension"},
	}
	for _, goalStateKey := range emptyGoalStateKeys {
		observer.goalStateEventMap.Store(goalStateKey, emptyStatus)
	}

	ctx.Log("message", "Getting all goal states from the event map with the latest status that are not empty")
	immediateTopLevelStatus := observer.getImmediateTopLevelStatusToReport()
	require.Equal(t, 1, len(immediateTopLevelStatus.AggregateHandlerImmediateStatus), "Only one handler should be reported in the immediate status")

	for _, handler := range immediateTopLevelStatus.AggregateHandlerImmediateStatus {
		require.Equal(t, "RunCommandHandler", handler.HandlerName, "Handler name should be the same")
		require.Equal(t, len(nonEmptyGoalStateKeys), len(handler.AggregateImmediateStatus), "All goal states should be reported")
		for _, immediateStatus := range handler.AggregateImmediateStatus {
			require.Equal(t, nonEmptyStatus, immediateStatus.Status, "Status should be the same")
			require.Equal(t, immediateStatus.TimestampUTC, immediateStatus.Status.TimestampUTC, "Timestamp should be the same")
			require.Contains(t, nonEmptyGoalStateKeys, types.GoalStateKey{SeqNumber: immediateStatus.SequenceNumber, ExtensionName: "testExtension"}, "Sequence number should be the same")
		}
	}
}

func Test_reportStatusToEndpointOk(t *testing.T) {
	ctx := log.NewContext(log.NewSyncLogger(log.NewLogfmtLogger(os.Stdout))).With("time", log.DefaultTimestamp)
	observer := StatusObserver{}
	observer.Initialize(ctx)
	observer.Reporter = statusreporter.TestGuestInformationClient{Endpoint: "localhost:3000/upload"}

	immediateStatus := ImmediateTopLevelStatus{
		AggregateHandlerImmediateStatus: []ImmediateHandlerStatus{
			{
				HandlerName: "testExtension",
				AggregateImmediateStatus: []ImmediateStatus{
					{
						SequenceNumber: 1,
						TimestampUTC:   "2021-09-01T12:00:00Z",
						Status:         types.StatusItem{},
					},
				},
			},
		},
	}

	err := observer.reportImmediateStatus(immediateStatus)
	require.Nil(t, err)
}

func Test_reportStatusToEndpointNotFound(t *testing.T) {
	ctx := log.NewContext(log.NewSyncLogger(log.NewLogfmtLogger(os.Stdout))).With("time", log.DefaultTimestamp)
	srv := httptest.NewServer(httpbin.GetMux())
	defer srv.Close()

	observer := StatusObserver{}
	observer.Initialize(ctx)
	observer.Reporter = statusreporter.NewGuestInformationServiceClient(srv.URL + "/uploadnotexistent")
	immediateStatus := ImmediateTopLevelStatus{
		AggregateHandlerImmediateStatus: []ImmediateHandlerStatus{
			{
				HandlerName: "testExtension",
				AggregateImmediateStatus: []ImmediateStatus{
					{
						SequenceNumber: 1,
						TimestampUTC:   "2021-09-01T12:00:00Z",
						Status:         types.StatusItem{},
					},
				},
			},
		},
	}

	err := observer.reportImmediateStatus(immediateStatus)
	require.ErrorContains(t, err, strconv.Itoa(http.StatusNotFound))
	require.ErrorContains(t, err, "Not Found")
}

func Test_getStatusForKey_statusFound(t *testing.T) {
	ctx := log.NewContext(log.NewSyncLogger(log.NewLogfmtLogger(os.Stdout))).With("time", log.DefaultTimestamp)
	observer := StatusObserver{}
	observer.Initialize(ctx)
	goalStateKey := types.GoalStateKey{SeqNumber: 1, ExtensionName: "testExtension"}
	statusItem := types.StatusItem{
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
	observer.goalStateEventMap.Store(goalStateKey, statusItem)
	status, ok := observer.GetStatusForKey(goalStateKey)
	require.True(t, ok)
	require.Equal(t, statusItem, status)
}

func Test_getStatusForKey_statusNotFound(t *testing.T) {
	ctx := log.NewContext(log.NewSyncLogger(log.NewLogfmtLogger(os.Stdout))).With("time", log.DefaultTimestamp)
	observer := StatusObserver{}
	observer.Initialize(ctx)
	goalStateKey := types.GoalStateKey{SeqNumber: 1, ExtensionName: "testExtension"}
	status, ok := observer.GetStatusForKey(goalStateKey)
	require.False(t, ok)
	require.Equal(t, types.StatusItem{}, status)
}

func Test_removeProcessedGoalStates_RemoveAll(t *testing.T) {
	ctx := log.NewContext(log.NewSyncLogger(log.NewLogfmtLogger(os.Stdout))).With("time", log.DefaultTimestamp)
	observer := StatusObserver{}
	observer.Initialize(ctx)

	ctx.Log("message", "Adding goal states to the event map")
	observer.goalStateEventMap.Store(types.GoalStateKey{SeqNumber: 1, ExtensionName: "testExtension"}, types.StatusItem{})
	observer.goalStateEventMap.Store(types.GoalStateKey{SeqNumber: 2, ExtensionName: "testExtension"}, types.StatusItem{})
	observer.goalStateEventMap.Store(types.GoalStateKey{SeqNumber: 3, ExtensionName: "testExtension"}, types.StatusItem{})

	ctx.Log("message", "Defining currentGoalStateKeys")
	currentGoalStateKeys := []types.GoalStateKey{}

	ctx.Log("message", "Removing goal states not in currentGoalStateKeys")
	observer.RemoveProcessedGoalStates(currentGoalStateKeys)

	require.Equal(t, 0, len(observer.getStatusForAllKeys()), "All goal states should be removed")
}

func Test_removeProcessedGoalStates_RemoveNone(t *testing.T) {
	ctx := log.NewContext(log.NewSyncLogger(log.NewLogfmtLogger(os.Stdout))).With("time", log.DefaultTimestamp)
	observer := StatusObserver{}
	observer.Initialize(ctx)

	ctx.Log("message", "Adding goal states to the event map")
	observer.goalStateEventMap.Store(types.GoalStateKey{SeqNumber: 1, ExtensionName: "testExtension"}, types.StatusItem{})
	observer.goalStateEventMap.Store(types.GoalStateKey{SeqNumber: 2, ExtensionName: "testExtension"}, types.StatusItem{})
	observer.goalStateEventMap.Store(types.GoalStateKey{SeqNumber: 3, ExtensionName: "testExtension"}, types.StatusItem{})

	ctx.Log("message", "Defining currentGoalStateKeys")
	currentGoalStateKeys := []types.GoalStateKey{
		{SeqNumber: 1, ExtensionName: "testExtension"},
		{SeqNumber: 2, ExtensionName: "testExtension"},
		{SeqNumber: 3, ExtensionName: "testExtension"},
	}

	ctx.Log("message", "Removing goal states not in currentGoalStateKeys")
	observer.RemoveProcessedGoalStates(currentGoalStateKeys)

	require.Equal(t, 3, len(observer.getStatusForAllKeys()), "No goal states should be removed")
}

func Test_removeProcessedGoalStates_RemoveOne(t *testing.T) {
	ctx := log.NewContext(log.NewSyncLogger(log.NewLogfmtLogger(os.Stdout))).With("time", log.DefaultTimestamp)
	observer := StatusObserver{}
	observer.Initialize(ctx)

	ctx.Log("message", "Adding goal states to the event map")
	observer.goalStateEventMap.Store(types.GoalStateKey{SeqNumber: 1, ExtensionName: "testExtension"}, types.StatusItem{})
	observer.goalStateEventMap.Store(types.GoalStateKey{SeqNumber: 2, ExtensionName: "testExtension"}, types.StatusItem{})
	observer.goalStateEventMap.Store(types.GoalStateKey{SeqNumber: 3, ExtensionName: "testExtension"}, types.StatusItem{})

	ctx.Log("message", "Defining currentGoalStateKeys")
	currentGoalStateKeys := []types.GoalStateKey{
		{SeqNumber: 1, ExtensionName: "testExtension"},
		{SeqNumber: 2, ExtensionName: "testExtension"},
	}

	ctx.Log("message", "Removing goal states not in currentGoalStateKeys")
	observer.RemoveProcessedGoalStates(currentGoalStateKeys)

	require.Equal(t, 2, len(observer.getStatusForAllKeys()), "One goal state should be removed")
}
