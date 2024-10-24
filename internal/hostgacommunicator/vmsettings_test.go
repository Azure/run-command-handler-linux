package hostgacommunicator

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"testing"

	"github.com/Azure/run-command-handler-linux/internal/requesthelper"
	"github.com/ahmetb/go-httpbin"
	"github.com/go-kit/kit/log"
	"github.com/stretchr/testify/require"
)

type TestUrlRequest struct {
	url string
}

func NewTestUrlRequest(url string) *TestUrlRequest {
	return &TestUrlRequest{url}
}

func (u *TestUrlRequest) GetRequest(ctx *log.Context, eTag string) (*http.Request, error) {
	return http.NewRequest("GET", u.url, nil)
}

type TestRequestManager struct {
	testUrlRequest *TestUrlRequest
}

func (li *TestRequestManager) GetVMSettingsRequestManager(ctx *log.Context) (*requesthelper.RequestManager, error) {
	return requesthelper.GetRequestManager(li.testUrlRequest, vmSettingsRequestTimeout), nil
}

func Test_GetImmediateVMSettingsFailedToParseJson(t *testing.T) {
	ctx := log.NewContext(log.NewSyncLogger(log.NewLogfmtLogger(os.Stdout))).With("time", log.DefaultTimestamp)
	srv := httptest.NewServer(httpbin.GetMux())
	defer srv.Close()

	testRequest := new(TestRequestManager)
	testRequest.testUrlRequest = NewTestUrlRequest(srv.URL + "/status/200") // ok with no valid response
	communicator := NewHostGACommunicator(testRequest)

	_, err := communicator.GetImmediateVMSettings(ctx, "")
	require.NotNil(t, err)
	require.ErrorContains(t, err, "failed to parse json")
}

func Test_GetImmediateVMSettingsHandleNotFound(t *testing.T) {
	ctx := log.NewContext(log.NewSyncLogger(log.NewLogfmtLogger(os.Stdout))).With("time", log.DefaultTimestamp)
	srv := httptest.NewServer(httpbin.GetMux())
	defer srv.Close()

	testRequest := new(TestRequestManager)
	testRequest.testUrlRequest = NewTestUrlRequest(srv.URL + "/status/404") // not found
	communicator := NewHostGACommunicator(testRequest)

	_, err := communicator.GetImmediateVMSettings(ctx, "")
	require.NotNil(t, err)
	require.ErrorContains(t, err, "metadata request failed with retries")
	require.ErrorContains(t, err, "404")
}

func Test_GetVMSettingsRequestManager(t *testing.T) {
	ctx := log.NewContext(log.NewSyncLogger(log.NewLogfmtLogger(os.Stdout))).With("time", log.DefaultTimestamp)
	requestManager, err := GetVMSettingsRequestManager(ctx)
	require.Nil(t, err)
	require.NotNil(t, requestManager)
}

func Test_FileExists(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "")
	defer os.RemoveAll(tmpDir)
	require.Nil(t, err)

	nonExistentFile := path.Join(tmpDir, "nonexistentfile.txt")
	existentFile := path.Join(tmpDir, "existentfile.txt")
	err = os.WriteFile(existentFile, []byte{}, 0700)
	require.Nil(t, err)

	require.False(t, fileExists(nonExistentFile))
	require.True(t, fileExists(existentFile))
}
