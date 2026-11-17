package status

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/Azure/run-command-handler-linux/internal/constants"
	"github.com/Azure/run-command-handler-linux/internal/handlersettings"
	"github.com/Azure/run-command-handler-linux/internal/types"
	"github.com/Azure/run-command-handler-linux/pkg/statusreporter"
	"github.com/ahmetb/go-httpbin"
	"github.com/go-kit/kit/log"
	"github.com/stretchr/testify/require"
)

type recordingImmediateStatusReporter struct {
	payloads   []string
	statusCode int
}

func (r *recordingImmediateStatusReporter) GetEndpoint() string {
	return "http://localhost"
}

func (r *recordingImmediateStatusReporter) GetPutStatusUri() string {
	return r.GetEndpoint() + "/immediateStatus"
}

func (r *recordingImmediateStatusReporter) ReportStatus(_ *log.Context, payload string) (*http.Response, error) {
	r.payloads = append(r.payloads, payload)
	recorder := httptest.NewRecorder()
	recorder.WriteHeader(r.statusCode)
	response := recorder.Result()
	response.Request = httptest.NewRequest(http.MethodPut, r.GetPutStatusUri(), nil)
	return response, nil
}

func setImmediateStatusTestEnvironment(t *testing.T, statusFolder string, writeEnvironment bool) {
	t.Helper()
	root := t.TempDir()
	t.Setenv(constants.ExtensionPathEnvName, root)
	require.NoError(t, os.Mkdir(filepath.Join(root, constants.ImmediateStatusFileDirectory), 0755))
	oldExecutable := os.Args[0]
	os.Args[0] = filepath.Join(root, "bin", "immediate-run-command-handler")
	require.NoError(t, os.Mkdir(filepath.Dir(os.Args[0]), 0755))
	t.Cleanup(func() { os.Args[0] = oldExecutable })
	if writeEnvironment {
		env := types.HandlerEnvironment{}
		env.HandlerEnvironment.StatusFolder = statusFolder
		data, err := json.Marshal([]types.HandlerEnvironment{env})
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(root, handlersettings.HandlerEnvFileName), data, 0600))
	}
}

func Test_onNotify_writesLocalDebugStatus(t *testing.T) {
	for _, statusType := range []types.StatusType{types.StatusTransitioning, types.StatusSuccess, types.StatusError, types.StatusSkipped} {
		t.Run(string(statusType), func(t *testing.T) {
			folder := t.TempDir()
			setImmediateStatusTestEnvironment(t, folder, true)
			ctx := log.NewContext(log.NewNopLogger())
			observer := StatusObserver{}
			observer.Initialize(ctx)
			reporter := &recordingImmediateStatusReporter{statusCode: http.StatusOK}
			observer.Reporter = reporter
			item, err := GetSingleStatusItem(ctx, statusType, types.CmdEnableTemplate,
				`{"exitCode":0,"output":"debug output","error":""}`, "immediate-inline")
			require.NoError(t, err)
			progress, err := GetSingleStatusItem(ctx, types.StatusTransitioning, types.CmdEnableTemplate, "running", "immediate-inline")
			require.NoError(t, err)
			event := types.StatusEventArgs{
				StatusKey:      types.GoalStateKey{HandlerName: constants.RunCommandTestExtensionName, ExtensionName: "immediate-inline", SeqNumber: 7, RuntimeSettingsState: "enabled"},
				TopLevelStatus: progress,
			}
			require.NoError(t, observer.OnNotify(event))
			require.FileExists(t, filepath.Join(folder, "immediate-inline.7.status"))
			event.TopLevelStatus = item
			require.NoError(t, observer.OnNotify(event))
			data, err := os.ReadFile(filepath.Join(folder, "immediate-inline.7.status"))
			require.NoError(t, err)
			var local types.StatusReport
			require.NoError(t, json.Unmarshal(data, &local))
			require.Equal(t, types.StatusReport{item}, local)
			require.Len(t, reporter.payloads, 2)
			var uploaded ImmediateTopLevelStatus
			require.NoError(t, json.Unmarshal([]byte(reporter.payloads[1]), &uploaded))
			require.Len(t, uploaded.AggregateHandlerImmediateStatus, 1)
			require.Equal(t, event.StatusKey.HandlerName, uploaded.AggregateHandlerImmediateStatus[0].HandlerName)
			require.Len(t, uploaded.AggregateHandlerImmediateStatus[0].AggregateImmediateStatus, 1)
			require.Equal(t, item.Status, uploaded.AggregateHandlerImmediateStatus[0].AggregateImmediateStatus[0].Status)
			require.Equal(t, item.TimestampUTC, uploaded.AggregateHandlerImmediateStatus[0].AggregateImmediateStatus[0].TimestampUTC)
			require.Equal(t, event.StatusKey.SeqNumber, uploaded.AggregateHandlerImmediateStatus[0].AggregateImmediateStatus[0].SequenceNumber)
		})
	}
}

func Test_onNotify_localDebugFailureDoesNotPreventHTTPReporting(t *testing.T) {
	for _, failure := range []string{"missing environment", "empty status directory", "invalid status directory", "rename failure"} {
		t.Run(failure, func(t *testing.T) {
			folder := t.TempDir()
			if failure == "invalid status directory" {
				folder = filepath.Join(folder, "not-a-directory")
				require.NoError(t, os.WriteFile(folder, []byte("fixture"), 0600))
			} else if failure == "empty status directory" {
				folder = ""
			} else if failure == "rename failure" {
				require.NoError(t, os.Mkdir(filepath.Join(folder, "immediate-inline.7.status"), 0755))
			}
			setImmediateStatusTestEnvironment(t, folder, failure != "missing environment")
			var logs bytes.Buffer
			ctx := log.NewContext(log.NewLogfmtLogger(&logs))
			observer := StatusObserver{}
			observer.Initialize(ctx)
			reporter := &recordingImmediateStatusReporter{statusCode: http.StatusOK}
			observer.Reporter = reporter
			item, err := GetSingleStatusItem(ctx, types.StatusSuccess, types.CmdEnableTemplate, "finished", "immediate-inline")
			require.NoError(t, err)
			require.NoError(t, observer.OnNotify(types.StatusEventArgs{
				StatusKey:      types.GoalStateKey{HandlerName: constants.RunCommandTestExtensionName, ExtensionName: "immediate-inline", SeqNumber: 7, RuntimeSettingsState: "enabled"},
				TopLevelStatus: item,
			}))
			require.Len(t, reporter.payloads, 1)
			require.Contains(t, reporter.payloads[0], "finished")
			require.Contains(t, logs.String(), "failed to save local immediate status debug copy")
		})
	}
}

func Test_onNotify_localDebugCopySurvivesHTTPFailure(t *testing.T) {
	folder := t.TempDir()
	setImmediateStatusTestEnvironment(t, folder, true)
	ctx := log.NewContext(log.NewNopLogger())
	observer := StatusObserver{}
	observer.Initialize(ctx)
	reporter := &recordingImmediateStatusReporter{statusCode: http.StatusInternalServerError}
	observer.Reporter = reporter
	item, err := GetSingleStatusItem(ctx, types.StatusSuccess, types.CmdEnableTemplate, "finished", "immediate-inline")
	require.NoError(t, err)
	err = observer.OnNotify(types.StatusEventArgs{
		StatusKey:      types.GoalStateKey{HandlerName: constants.RunCommandTestExtensionName, ExtensionName: "immediate-inline", SeqNumber: 7, RuntimeSettingsState: "enabled"},
		TopLevelStatus: item,
	})
	require.ErrorContains(t, err, "500")
	require.FileExists(t, filepath.Join(folder, "immediate-inline.7.status"))
}

func Test_onNotify_emptyStatusDoesNotOverwriteLocalDebugCopy(t *testing.T) {
	folder := t.TempDir()
	setImmediateStatusTestEnvironment(t, folder, true)
	path := filepath.Join(folder, "immediate-inline.7.status")
	require.NoError(t, os.WriteFile(path, []byte("previous status"), 0600))
	observer := StatusObserver{}
	observer.Initialize(log.NewContext(log.NewNopLogger()))
	observer.Reporter = &recordingImmediateStatusReporter{statusCode: http.StatusOK}
	require.NoError(t, observer.OnNotify(types.StatusEventArgs{
		StatusKey: types.GoalStateKey{HandlerName: constants.RunCommandTestExtensionName, ExtensionName: "immediate-inline", SeqNumber: 7, RuntimeSettingsState: "enabled"},
	}))
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "previous status", string(data))
}

func Test_onNotify_requiresHandlerName(t *testing.T) {
	observer := StatusObserver{}
	observer.Initialize(log.NewContext(log.NewNopLogger()))
	reporter := &recordingImmediateStatusReporter{statusCode: http.StatusOK}
	observer.Reporter = reporter
	err := observer.OnNotify(types.StatusEventArgs{
		StatusKey: types.GoalStateKey{ExtensionName: "immediate-inline", SeqNumber: 1},
	})
	require.ErrorContains(t, err, "without a handler name")
	require.Empty(t, reporter.payloads)
	require.Empty(t, observer.getStatusForAllKeys())
}

func Test_immediateStatus_preservesHandlerAcrossAggregationAndRestart(t *testing.T) {
	setImmediateStatusTestEnvironment(t, t.TempDir(), true)
	ctx := log.NewContext(log.NewNopLogger())
	observer := StatusObserver{}
	observer.Initialize(ctx)
	expected := map[string]ImmediateStatus{}
	for _, handler := range []string{constants.RunCommandExtensionName, constants.RunCommandTestExtensionName} {
		item, err := GetSingleStatusItem(ctx, types.StatusTransitioning, types.CmdEnableTemplate, handler, "same-command")
		require.NoError(t, err)
		observer.goalStateEventMap.Store(types.GoalStateKey{
			HandlerName: handler, ExtensionName: "same-command", SeqNumber: 1, RuntimeSettingsState: "enabled",
		}, item)
		expected[handler] = ImmediateStatus{HandlerName: handler, SequenceNumber: 1, TimestampUTC: item.TimestampUTC, Status: item.Status}
	}
	report := observer.getImmediateTopLevelStatusToReport()
	require.Len(t, report.AggregateHandlerImmediateStatus, 2)
	for _, handler := range report.AggregateHandlerImmediateStatus {
		require.Equal(t, []ImmediateStatus{expected[handler.HandlerName]}, handler.AggregateImmediateStatus)
	}
	// Complete both commands with the same resource name and sequence number.
	observer.goalStateEventMap.Range(func(key, value interface{}) bool {
		item := value.(types.StatusItem)
		item.Status.Status = types.StatusSuccess
		observer.goalStateEventMap.Store(key, item)
		status := expected[key.(types.GoalStateKey).HandlerName]
		status.Status = item.Status
		expected[status.HandlerName] = status
		return true
	})
	report = observer.getImmediateTopLevelStatusToReport()
	require.Empty(t, observer.getStatusForAllKeys())
	require.Len(t, report.AggregateHandlerImmediateStatus, 2)
	restarted := StatusObserver{}
	restarted.Initialize(ctx)
	reporter := &recordingImmediateStatusReporter{statusCode: http.StatusOK}
	restarted.Reporter = reporter
	require.NoError(t, restarted.OnDemandNotify())
	require.Len(t, reporter.payloads, 1)
	var uploaded ImmediateTopLevelStatus
	require.NoError(t, json.Unmarshal([]byte(reporter.payloads[0]), &uploaded))
	require.Len(t, uploaded.AggregateHandlerImmediateStatus, 2)
	for _, handler := range uploaded.AggregateHandlerImmediateStatus {
		require.Contains(t, expected, handler.HandlerName)
		require.Len(t, handler.AggregateImmediateStatus, 1)
		status := handler.AggregateImmediateStatus[0]
		require.Equal(t, expected[handler.HandlerName].Status, status.Status)
		require.Equal(t, 1, status.SequenceNumber)
	}
	var wire map[string][]map[string]json.RawMessage
	require.NoError(t, json.Unmarshal([]byte(reporter.payloads[0]), &wire))
	for _, handler := range wire["aggregateHandlerImmediateStatus"] {
		var statuses []map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(handler["aggregateImmediateStatus"], &statuses))
		require.NotContains(t, statuses[0], "handlerName", "handler identity belongs only on the enclosing handler")
	}
}

func Test_terminalStatusRemoval_isScopedToHandler(t *testing.T) {
	for _, runtimeState := range []string{"enabled", "disabled"} {
		t.Run(runtimeState, func(t *testing.T) {
			setImmediateStatusTestEnvironment(t, t.TempDir(), true)
			ctx := log.NewContext(log.NewNopLogger())
			cached := []ImmediateStatus{}
			for _, handler := range []string{constants.RunCommandExtensionName, constants.RunCommandTestExtensionName} {
				cached = append(cached, ImmediateStatus{
					HandlerName: handler, SequenceNumber: 1,
					Status: types.Status{Name: "same-command", Status: types.StatusSuccess},
				})
			}
			require.NoError(t, SaveGoalStatesInTerminalStatus(ctx, cached))
			seq := 1
			if runtimeState == "enabled" {
				seq = 2
			}
			require.NoError(t, RemoveDisabledAndUpdatedGoalStatesInLocalStatusFile(ctx, []types.GoalStateKey{{
				HandlerName: constants.RunCommandTestExtensionName, ExtensionName: "same-command",
				SeqNumber: seq, RuntimeSettingsState: runtimeState,
			}}))
			remaining, err := GetGoalStatesInTerminalStatus(ctx)
			require.NoError(t, err)
			require.Equal(t, cached[:1], remaining)
		})
	}
}

func Test_removeProcessedGoalStates_preservesHandlerName(t *testing.T) {
	setImmediateStatusTestEnvironment(t, t.TempDir(), true)
	ctx := log.NewContext(log.NewNopLogger())
	observer := StatusObserver{}
	observer.Initialize(ctx)
	reporter := &recordingImmediateStatusReporter{statusCode: http.StatusOK}
	observer.Reporter = reporter
	item, err := GetSingleStatusItem(ctx, types.StatusTransitioning, types.CmdEnableTemplate, "running", "immediate-inline")
	require.NoError(t, err)
	observer.goalStateEventMap.Store(types.GoalStateKey{
		HandlerName: constants.RunCommandTestExtensionName, ExtensionName: "immediate-inline",
		SeqNumber: 1, RuntimeSettingsState: "enabled",
	}, item)
	observer.RemoveProcessedGoalStates(nil)
	cached, err := GetGoalStatesInTerminalStatus(ctx)
	require.NoError(t, err)
	require.Len(t, cached, 1)
	require.Equal(t, constants.RunCommandTestExtensionName, cached[0].HandlerName)
	require.Len(t, reporter.payloads, 1)
	var uploaded ImmediateTopLevelStatus
	require.NoError(t, json.Unmarshal([]byte(reporter.payloads[0]), &uploaded))
	require.Len(t, uploaded.AggregateHandlerImmediateStatus, 1)
	require.Equal(t, constants.RunCommandTestExtensionName, uploaded.AggregateHandlerImmediateStatus[0].HandlerName)
}

func Test_getTerminalStatus_doesNotGuessLegacyHandlerName(t *testing.T) {
	setImmediateStatusTestEnvironment(t, t.TempDir(), true)
	legacy, err := json.Marshal([]ImmediateStatus{{
		SequenceNumber: 1, Status: types.Status{Name: "legacy-command", Status: types.StatusSuccess},
	}})
	require.NoError(t, err)
	path := filepath.Join(os.Getenv(constants.ExtensionPathEnvName), constants.ImmediateStatusFileDirectory,
		constants.ImmediateGoalStatesInTerminalStatusFileName)
	require.NoError(t, os.WriteFile(path, legacy, 0600))
	var logs bytes.Buffer
	items, err := GetGoalStatesInTerminalStatus(log.NewContext(log.NewLogfmtLogger(&logs)))
	require.NoError(t, err)
	require.Empty(t, items)
	require.Contains(t, logs.String(), "ignoring cached immediate status without a handler name")
}

func Test_onNotify_success(t *testing.T) {
	ctx := log.NewContext(log.NewSyncLogger(log.NewLogfmtLogger(os.Stdout))).With("time", log.DefaultTimestamp)
	observer := StatusObserver{}
	observer.Initialize(ctx)
	observer.Reporter = statusreporter.TestGuestInformationClient{Endpoint: "localhost:3000/upload"}

	emptyStatusItem := types.StatusItem{}
	observer.goalStateEventMap.Store(types.GoalStateKey{HandlerName: constants.RunCommandTestExtensionName, SeqNumber: 1, ExtensionName: "testExtension"}, emptyStatusItem)

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
		StatusKey:      types.GoalStateKey{HandlerName: constants.RunCommandTestExtensionName, SeqNumber: 1, ExtensionName: "testExtension"},
		TopLevelStatus: nonEmptyStatusItem,
	}
	err := observer.OnNotify(topStatus)
	require.Nil(t, err, "OnNotify should not return an error")
	status, ok := observer.GetStatusForKey(types.GoalStateKey{HandlerName: constants.RunCommandTestExtensionName, SeqNumber: 1, ExtensionName: "testExtension"})
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
	observer.goalStateEventMap.Store(types.GoalStateKey{HandlerName: constants.RunCommandTestExtensionName, SeqNumber: 1, ExtensionName: "testExtension"}, emptyStatusItem)

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
		StatusKey:      types.GoalStateKey{HandlerName: constants.RunCommandTestExtensionName, SeqNumber: 1, ExtensionName: "testExtension"},
		TopLevelStatus: nonEmptyStatusItem,
	}
	err := observer.OnNotify(topStatus)
	require.NotNil(t, err, "OnNotify should return an error")
	require.Contains(t, err.Error(), strconv.Itoa(http.StatusNotFound))

	status, ok := observer.GetStatusForKey(types.GoalStateKey{HandlerName: constants.RunCommandTestExtensionName, SeqNumber: 1, ExtensionName: "testExtension"})
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
		{HandlerName: constants.RunCommandTestExtensionName, SeqNumber: 1, ExtensionName: "testExtension"},
		{HandlerName: constants.RunCommandTestExtensionName, SeqNumber: 2, ExtensionName: "testExtension"},
		{HandlerName: constants.RunCommandTestExtensionName, SeqNumber: 3, ExtensionName: "testExtension"},
	}
	for _, goalStateKey := range goalStateKeys {
		observer.goalStateEventMap.Store(goalStateKey, nonEmptyStatus)
	}

	ctx.Log("message", "Getting all goal states from the event map with the latest status that are not empty")
	immediateTopLevelStatus := observer.getImmediateTopLevelStatusToReport()
	require.Equal(t, 1, len(immediateTopLevelStatus.AggregateHandlerImmediateStatus), "Only one handler should be reported in the immediate status")

	for _, handler := range immediateTopLevelStatus.AggregateHandlerImmediateStatus {
		require.Equal(t, constants.RunCommandTestExtensionName, handler.HandlerName, "Handler name should be the same")
		require.Equal(t, len(goalStateKeys), len(handler.AggregateImmediateStatus), "All goal states should be reported")
		for _, immediateStatus := range handler.AggregateImmediateStatus {
			require.Equal(t, nonEmptyStatus.Status, immediateStatus.Status, "Status should be the same")
			require.Equal(t, immediateStatus.TimestampUTC, immediateStatus.TimestampUTC, "Timestamp should be the same")
			require.Contains(t, goalStateKeys, types.GoalStateKey{HandlerName: handler.HandlerName, SeqNumber: immediateStatus.SequenceNumber, ExtensionName: "testExtension"}, "Sequence number should be the same")
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
		{HandlerName: constants.RunCommandTestExtensionName, SeqNumber: 1, ExtensionName: "testExtension"},
		{HandlerName: constants.RunCommandTestExtensionName, SeqNumber: 2, ExtensionName: "testExtension"},
		{HandlerName: constants.RunCommandTestExtensionName, SeqNumber: 3, ExtensionName: "testExtension"},
	}
	for _, goalStateKey := range goalStateKeys {
		observer.goalStateEventMap.Store(goalStateKey, emptyStatus)
	}

	ctx.Log("message", "Getting all goal states from the event map with the latest status that are not empty")
	immediateTopLevelStatus := observer.getImmediateTopLevelStatusToReport()
	require.Equal(t, 1, len(immediateTopLevelStatus.AggregateHandlerImmediateStatus), "Only one handler should be reported in the immediate status")

	for _, handler := range immediateTopLevelStatus.AggregateHandlerImmediateStatus {
		require.Equal(t, constants.RunCommandTestExtensionName, handler.HandlerName, "Handler name should be the same")
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
		{HandlerName: constants.RunCommandTestExtensionName, SeqNumber: 1, ExtensionName: "testExtension"},
		{HandlerName: constants.RunCommandTestExtensionName, SeqNumber: 2, ExtensionName: "testExtension"},
		{HandlerName: constants.RunCommandTestExtensionName, SeqNumber: 3, ExtensionName: "testExtension"},
	}
	for _, goalStateKey := range nonEmptyGoalStateKeys {
		observer.goalStateEventMap.Store(goalStateKey, nonEmptyStatus)
	}

	emptyGoalStateKeys := []types.GoalStateKey{
		{HandlerName: constants.RunCommandTestExtensionName, SeqNumber: 4, ExtensionName: "testExtension"},
		{HandlerName: constants.RunCommandTestExtensionName, SeqNumber: 5, ExtensionName: "testExtension"},
		{HandlerName: constants.RunCommandTestExtensionName, SeqNumber: 6, ExtensionName: "testExtension"},
	}
	for _, goalStateKey := range emptyGoalStateKeys {
		observer.goalStateEventMap.Store(goalStateKey, emptyStatus)
	}

	ctx.Log("message", "Getting all goal states from the event map with the latest status that are not empty")
	immediateTopLevelStatus := observer.getImmediateTopLevelStatusToReport()
	require.Equal(t, 1, len(immediateTopLevelStatus.AggregateHandlerImmediateStatus), "Only one handler should be reported in the immediate status")

	for _, handler := range immediateTopLevelStatus.AggregateHandlerImmediateStatus {
		require.Equal(t, constants.RunCommandTestExtensionName, handler.HandlerName, "Handler name should be the same")
		require.Equal(t, len(nonEmptyGoalStateKeys), len(handler.AggregateImmediateStatus), "All goal states should be reported")
		for _, immediateStatus := range handler.AggregateImmediateStatus {
			require.Equal(t, nonEmptyStatus.Status, immediateStatus.Status, "Status should be the same")
			require.Equal(t, immediateStatus.TimestampUTC, immediateStatus.TimestampUTC, "Timestamp should be the same")
			require.Contains(t, nonEmptyGoalStateKeys, types.GoalStateKey{HandlerName: handler.HandlerName, SeqNumber: immediateStatus.SequenceNumber, ExtensionName: "testExtension"}, "Sequence number should be the same")
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
						Status:         types.Status{},
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
						Status:         types.Status{},
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
	ctx.Log("msg", "Creating status observer")
	observer := StatusObserver{}
	observer.Initialize(ctx)
	observer.Reporter = statusreporter.TestGuestInformationClient{Endpoint: "localhost:3000/upload"}

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
	ctx.Log("msg", "Creating status observer")
	observer := StatusObserver{}
	observer.Initialize(ctx)
	observer.Reporter = statusreporter.TestGuestInformationClient{Endpoint: "localhost:3000/upload"}

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
	ctx.Log("msg", "Creating status observer")
	observer := StatusObserver{}
	observer.Initialize(ctx)
	observer.Reporter = statusreporter.TestGuestInformationClient{Endpoint: "localhost:3000/upload"}

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
