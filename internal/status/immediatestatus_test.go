package status

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/Azure/run-command-handler-linux/internal/constants"
	"github.com/Azure/run-command-handler-linux/internal/types"
	"github.com/Azure/run-command-handler-linux/pkg/statusreporter"
	"github.com/ahmetb/go-httpbin"
	"github.com/go-kit/kit/log"
	"github.com/stretchr/testify/require"
)

func Test_reportStatus_fails(t *testing.T) {
	fakeEnv := types.HandlerEnvironment{}
	fakeEnv.HandlerEnvironment.StatusFolder = "/non-existing/dir/"

	metadata := types.NewRCMetadata("", 1, constants.DownloadFolder, constants.DataDir)
	err := ReportStatusToLocalFile(log.NewContext(log.NewNopLogger()), fakeEnv, metadata, types.StatusSuccess, types.CmdEnableTemplate, "")
	require.NotNil(t, err)
	require.Contains(t, err.Error(), "failed to save handler status")
}

func Test_reportStatus_fileExists(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "")
	require.Nil(t, err)
	defer os.RemoveAll(tmpDir)

	extName := "first"
	fakeEnv := types.HandlerEnvironment{}
	fakeEnv.HandlerEnvironment.StatusFolder = tmpDir

	metadata := types.NewRCMetadata(extName, 1, constants.DownloadFolder, constants.DataDir)
	require.Nil(t, ReportStatusToLocalFile(log.NewContext(log.NewNopLogger()), fakeEnv, metadata, types.StatusError, types.CmdEnableTemplate, "FOO ERROR"))

	path := filepath.Join(tmpDir, "first.1.status")
	b, err := os.ReadFile(path)
	require.Nil(t, err, ".status file exists")
	require.NotEqual(t, 0, len(b), ".status file not empty")
}

func Test_reportStatus_checksIfShouldBeReported(t *testing.T) {
	for _, c := range types.CmdTemplates {
		tmpDir, err := os.MkdirTemp("", "status-"+c.Name)
		require.Nil(t, err)
		defer os.RemoveAll(tmpDir)

		extName := "first"
		fakeEnv := types.HandlerEnvironment{}
		fakeEnv.HandlerEnvironment.StatusFolder = tmpDir
		metadata := types.NewRCMetadata(extName, 2, constants.DownloadFolder, constants.DataDir)
		require.Nil(t, ReportStatusToLocalFile(log.NewContext(log.NewNopLogger()), fakeEnv, metadata, types.StatusSuccess, c, ""))

		fp := filepath.Join(tmpDir, "first.2.status")
		_, err = os.Stat(fp) // check if the .status file is there
		if c.ShouldReportStatus && err != nil {
			t.Fatalf("cmd=%q should have reported status file=%q err=%v", c.Name, fp, err)
		}
		if !c.ShouldReportStatus {
			if err == nil {
				t.Fatalf("cmd=%q should not have reported status file. file=%q", c.Name, fp)
			} else if !os.IsNotExist(err) {
				t.Fatalf("cmd=%q some other error occurred. file=%q err=%q", c.Name, fp, err)
			}
		}
	}
}

type TestGuestInformationClient struct {
	endpoint string
}

func (c TestGuestInformationClient) GetEndpoint() string {
	return c.endpoint
}

func (c TestGuestInformationClient) GetPutStatusUri() string {
	return fmt.Sprintf(constants.PutImmediateStatusFormatString, c.GetEndpoint())
}

func (c TestGuestInformationClient) ReportStatus(statusToUpload string) (*http.Response, error) {
	w := httptest.NewRecorder()
	resp := w.Result()
	resp.Request = httptest.NewRequest(http.MethodPut, c.endpoint, nil)
	return resp, nil
}

func Test_ReportStatusToEndpointOk(t *testing.T) {
	ctx := log.NewContext(log.NewSyncLogger(log.NewLogfmtLogger(os.Stdout))).With("time", log.DefaultTimestamp)
	observer := StatusObserver{}
	observer.Initialize(ctx)
	observer.reporter = TestGuestInformationClient{endpoint: "localhost:3000/upload"}

	immediateStatus := ImmediateTopLevelStatus{
		AggregateHandlerImmediateStatus: []ImmediateHandlerStatus{
			ImmediateHandlerStatus{
				HandlerName: "testExtension",
				AggregateImmediateStatus: []ImmediateStatus{
					ImmediateStatus{
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

func Test_ReportStatusToEndpointNotFound(t *testing.T) {
	ctx := log.NewContext(log.NewSyncLogger(log.NewLogfmtLogger(os.Stdout))).With("time", log.DefaultTimestamp)
	srv := httptest.NewServer(httpbin.GetMux())
	defer srv.Close()

	observer := StatusObserver{}
	observer.Initialize(ctx)
	observer.reporter = statusreporter.NewGuestInformationServiceClient(srv.URL + "/uploadnotexistent")
	immediateStatus := ImmediateTopLevelStatus{
		AggregateHandlerImmediateStatus: []ImmediateHandlerStatus{
			ImmediateHandlerStatus{
				HandlerName: "testExtension",
				AggregateImmediateStatus: []ImmediateStatus{
					ImmediateStatus{
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
