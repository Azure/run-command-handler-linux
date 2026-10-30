package status

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strconv"
	"testing"

	"github.com/Azure/run-command-handler-linux/internal/types"
	"github.com/Azure/run-command-handler-linux/pkg/errorutil"
	"github.com/Azure/run-command-handler-linux/pkg/statusreporter"
	"github.com/go-kit/kit/log"
	"github.com/stretchr/testify/require"
)

// errorutil.Internal_failedToMarshalTerminalGoalStates, errorutil.Internal_failedToMarshalStatusReport and errorutil.Internal_failedToMarshalImmediateStatus cannot be raised: their JSON payloads contain only
// strings, integers and slices of those structs (no custom marshalers).
// errorutil.Internal_unexpectedStatusItemCount cannot be raised: NewStatusReport always constructs exactly one item.
// errorutil.Internal_failedToMarshalInstanceView in instanceview.SerializeInstanceView likewise marshals only scalar
// fields; ClarificationError is explicitly excluded from JSON.

type mockTemporaryStatus struct{ closeErr error }

func (f mockTemporaryStatus) Name() string { return "/mock/status.tmp" }
func (f mockTemporaryStatus) Close() error { return f.closeErr }

func mockStatusFilesystem(t *testing.T) {
	t.Helper()
	create, write, read, rename, stat, remove := statusCreateTemp, statusWriteFile, statusReadFile, statusRename, statusStat, statusRemove
	t.Cleanup(func() {
		statusCreateTemp, statusWriteFile, statusReadFile, statusRename, statusStat, statusRemove = create, write, read, rename, stat, remove
	})
	statusCreateTemp = func(string, string) (temporaryStatusFile, error) { return mockTemporaryStatus{}, nil }
	statusWriteFile = func(string, []byte, os.FileMode) error { return nil }
	statusReadFile = func(string) ([]byte, error) { return []byte("[]"), nil }
	statusRename = func(string, string) error { return nil }
	statusStat = func(string) (os.FileInfo, error) { return nil, nil }
	statusRemove = func(string) error { return nil }
}

func TestStatusFilesystemClarifications(t *testing.T) {
	failure := errors.New("mock disk failure")
	ctx := log.NewContext(log.NewNopLogger())
	for _, code := range []int{errorutil.Os_FailedToCreateTemporaryStatusFile, errorutil.Os_FailedToCloseTemporaryStatusFile, errorutil.Os_FailedToWriteTemporaryStatusFile, errorutil.Os_FailedToPublishStatusFile, errorutil.Os_FailedToWriteTerminalGoalStates, errorutil.Os_FailedToPublishTerminalGoalStates, errorutil.Os_FailedToReadTerminalGoalStates, errorutil.Os_InvalidTerminalGoalStatesJson, errorutil.Os_FailedToStatTerminalGoalStates, errorutil.Internal_invalidGoalStateRemovalState} {
		t.Run(strconv.Itoa(code), func(t *testing.T) {
			mockStatusFilesystem(t)
			var err error
			switch code {
			case errorutil.Os_FailedToCreateTemporaryStatusFile:
				statusCreateTemp = func(string, string) (temporaryStatusFile, error) { return nil, failure }
			case errorutil.Os_FailedToCloseTemporaryStatusFile:
				statusCreateTemp = func(string, string) (temporaryStatusFile, error) { return mockTemporaryStatus{failure}, nil }
			case errorutil.Os_FailedToWriteTemporaryStatusFile, errorutil.Os_FailedToWriteTerminalGoalStates:
				statusWriteFile = func(string, []byte, os.FileMode) error { return failure }
			case errorutil.Os_FailedToPublishStatusFile, errorutil.Os_FailedToPublishTerminalGoalStates:
				statusRename = func(string, string) error { return failure }
			case errorutil.Os_FailedToReadTerminalGoalStates:
				statusReadFile = func(string) ([]byte, error) { return nil, failure }
			case errorutil.Os_InvalidTerminalGoalStatesJson:
				statusReadFile = func(string) ([]byte, error) { return []byte("{invalid"), nil }
			case errorutil.Os_FailedToStatTerminalGoalStates:
				statusStat = func(string) (os.FileInfo, error) { return nil, failure }
			}
			switch code {
			case errorutil.Os_FailedToCreateTemporaryStatusFile, errorutil.Os_FailedToCloseTemporaryStatusFile, errorutil.Os_FailedToWriteTemporaryStatusFile, errorutil.Os_FailedToPublishStatusFile:
				err = SaveStatusReport("/mock", "command", 1, []byte("[]"))
			case errorutil.Os_FailedToWriteTerminalGoalStates, errorutil.Os_FailedToPublishTerminalGoalStates:
				err = SaveGoalStatesInTerminalStatus(ctx, nil)
			case errorutil.Internal_invalidGoalStateRemovalState:
				err = RemoveDisabledAndUpdatedGoalStatesInLocalStatusFile(ctx, []types.GoalStateKey{{RuntimeSettingsState: "invalid"}})
			default:
				_, err = GetGoalStatesInTerminalStatus(ctx)
			}
			actual, ok := errorutil.Code(err)
			require.True(t, ok, "%v", err)
			require.Equal(t, code, actual)
		})
	}
}

func TestClarificationsInManagedAndImmediateStatus(t *testing.T) {
	for _, topLevel := range []types.StatusType{types.StatusSuccess, types.StatusError} {
		for _, code := range []int{errorutil.CommandExecution_failureExitCode, errorutil.Internal_unclassifiedCommandFailure} {
			t.Run(string(topLevel)+strconv.Itoa(code), func(t *testing.T) {
				mockStatusFilesystem(t)
				ctx := log.NewContext(log.NewNopLogger())
				cause := errorutil.WithCode(code, errors.New("original execution error"))
				metadata := types.NewRCMetadata("command", 9, "download", "/mock")
				metadata.ClarificationError = cause
				env := types.HandlerEnvironment{}
				env.HandlerEnvironment.StatusFolder = "/mock/status"
				var written []byte
				statusWriteFile = func(_ string, data []byte, _ os.FileMode) error { written = data; return nil }
				require.NoError(t, ReportStatusToLocalFile(ctx, env, metadata, topLevel, types.CmdEnableTemplate, "instance view JSON"))
				var managed types.StatusReport
				require.NoError(t, json.Unmarshal(written, &managed))
				immediate, err := GetSingleStatusItem(ctx, topLevel, types.CmdEnableTemplate, "instance view JSON", "command", cause)
				require.NoError(t, err)
				var uploaded string
				statusObserver := &StatusObserver{}
				statusObserver.Initialize(ctx)
				statusObserver.Reporter = mockStatusReporter{
					response: &http.Response{StatusCode: http.StatusOK, Status: "200 OK"},
					payload:  &uploaded,
				}
				require.NoError(t, statusObserver.OnNotify(types.StatusEventArgs{
					StatusKey:      types.GoalStateKey{ExtensionName: "command", SeqNumber: 9, RuntimeSettingsState: "enabled"},
					TopLevelStatus: immediate,
				}))
				var aggregate ImmediateTopLevelStatus
				require.NoError(t, json.Unmarshal([]byte(uploaded), &aggregate))
				require.Len(t, aggregate.AggregateHandlerImmediateStatus, 1)
				require.Len(t, aggregate.AggregateHandlerImmediateStatus[0].AggregateImmediateStatus, 1)
				wireStatus := aggregate.AggregateHandlerImmediateStatus[0].AggregateImmediateStatus[0].Status
				for _, item := range []types.StatusItem{managed[0], immediate, {Status: wireStatus}} {
					require.Equal(t, topLevel, item.Status.Status)
					require.Equal(t, "instance view JSON", item.Status.FormattedMessage.Message)
					requireStatusClarification(t, item.Status, code)
				}
			})
		}
	}
	normal := types.NewStatusReport(types.StatusSuccess, "Enable", "success", "command")
	require.Empty(t, normal[0].Status.Substatuses)
}

func requireStatusClarification(t *testing.T, reported types.Status, want errorutil.ErrorClarificationCode) {
	t.Helper()
	require.Len(t, reported.Substatuses, 1)
	require.Equal(t, "ErrorClarification", reported.Substatuses[0].Name)
	require.Equal(t, string(types.StatusError), reported.Substatuses[0].Status)
	require.Equal(t, want, reported.Substatuses[0].Code)
}

type mockStatusReporter struct {
	statusreporter.IGuestInformationServiceClient
	err      error
	response *http.Response
	payload  *string
}

func (r mockStatusReporter) GetPutStatusUri() string { return "http://mock/status" }
func (r mockStatusReporter) ReportStatus(_ *log.Context, payload string) (*http.Response, error) {
	if r.payload != nil {
		*r.payload = payload
	}
	return r.response, r.err
}

func TestImmediateStatusReportingErrors(t *testing.T) {
	for _, code := range []int{errorutil.HostGA_failedToReportImmediateStatus, errorutil.HostGA_immediateStatusReturnedErrorStatus} {
		t.Run(strconv.Itoa(code), func(t *testing.T) {
			reporter := mockStatusReporter{response: &http.Response{StatusCode: 500, Status: "500 Internal Server Error"}}
			if code == errorutil.HostGA_failedToReportImmediateStatus {
				reporter.response = nil
				reporter.err = errors.New("transport failed")
			}
			observer := StatusObserver{ctx: log.NewContext(log.NewNopLogger()), Reporter: reporter}
			err := observer.reportImmediateStatus(ImmediateTopLevelStatus{})
			actual, ok := errorutil.Code(err)
			require.True(t, ok)
			require.Equal(t, code, actual)
		})
	}
}
