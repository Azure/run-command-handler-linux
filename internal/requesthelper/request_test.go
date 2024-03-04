package requesthelper_test

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/Azure/run-command-handler-linux/internal/requesthelper"
	"github.com/ahmetalpbalkan/go-httpbin"
	"github.com/go-kit/kit/log"
	"github.com/stretchr/testify/require"
)

const (
	testRequestTimeout   = 2 * time.Second
	badRequestorErrorMsg = "expected error from bad request"
	requestErrorMsg      = "expected error from request"
)

type RequestError struct {
	timeout   bool
	temporary bool
}

func NewErrorRequest(isTemporary bool, isTimeout bool) *ErrorDownloader {
	err := RequestError{isTimeout, isTemporary}
	return &ErrorDownloader{0, err}
}

func (u RequestError) Error() string {
	return requestErrorMsg
}

func (u RequestError) Temporary() bool {
	return u.temporary
}

func (u RequestError) Timeout() bool {
	return u.timeout
}

type TestUrlRequest struct {
	calls int
	url   string
}

func NewTestURLRequest(url string) *TestUrlRequest {
	return &TestUrlRequest{0, url}
}

func (u *TestUrlRequest) GetRequest(ctx *log.Context) (*http.Request, error) {
	u.calls++
	return http.NewRequest("GET", u.url, nil)
}

type ErrorDownloader struct {
	calls int
	err   RequestError
}

func (e *ErrorDownloader) GetRequest(ctx *log.Context) (*http.Request, error) {
	e.calls++
	return nil, e.err
}

type BadRequestor struct {
	calls int
}

func (b *BadRequestor) GetRequest(ctx *log.Context) (*http.Request, error) {
	b.calls++
	return nil, errors.New(badRequestorErrorMsg)
}

func TestMakeRequest_WrapsGetRequestError(t *testing.T) {
	ctx := log.NewContext(log.NewSyncLogger(log.NewLogfmtLogger(os.Stdout))).With("time", log.DefaultTimestamp)
	badRequestor := new(BadRequestor)
	rm := requesthelper.GetRequestManager(badRequestor, testRequestTimeout)
	_, err := rm.MakeRequest(ctx)
	require.Equal(t, badRequestor.calls, 1)
	require.NotNil(t, err)
	require.EqualError(t, err, badRequestorErrorMsg)
}

func TestMakeRequest_WrapsHttpBadUrlError(t *testing.T) {
	ctx := log.NewContext(log.NewSyncLogger(log.NewLogfmtLogger(os.Stdout))).With("time", log.DefaultTimestamp)
	testUrlRequest := NewTestURLRequest("bad url")
	rm := requesthelper.GetRequestManager(testUrlRequest, testRequestTimeout)
	_, err := rm.MakeRequest(ctx)
	require.Equal(t, testUrlRequest.calls, 1)
	require.NotNil(t, err)
	require.Contains(t, err.Error(), "unsupported protocol scheme")
}

func TestMakeRequest_RequestTimeout(t *testing.T) {
	ctx := log.NewContext(log.NewSyncLogger(log.NewLogfmtLogger(os.Stdout))).With("time", log.DefaultTimestamp)
	srv := httptest.NewServer(httpbin.GetMux())
	defer srv.Close()

	timeoutTestUrl := NewTestURLRequest(fmt.Sprintf("%s/delay/%d", srv.URL, 3)) // request to local server with delay
	rm := requesthelper.GetRequestManager(timeoutTestUrl, testRequestTimeout)
	_, err := rm.MakeRequest(ctx)

	require.Equal(t, timeoutTestUrl.calls, 1)
	require.NotNil(t, err)
	require.Contains(t, err.Error(), "Timeout exceeded")

	testUrl := NewTestURLRequest(fmt.Sprintf("%s/delay/%d", srv.URL, 1))
	rm = requesthelper.GetRequestManager(testUrl, testRequestTimeout)
	resp, err := rm.MakeRequest(ctx)

	require.Equal(t, testUrl.calls, 1)
	require.Nil(t, err)
	defer resp.Body.Close()
	require.NotNil(t, resp.Body)
}

func TestMakeRequest_BadStatusCodeFails(t *testing.T) {
	ctx := log.NewContext(log.NewSyncLogger(log.NewLogfmtLogger(os.Stdout))).With("time", log.DefaultTimestamp)
	srv := httptest.NewServer(httpbin.GetMux())
	defer srv.Close()

	for _, code := range []int{
		http.StatusNotFound,
		http.StatusForbidden,
		http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusBadRequest,
		http.StatusUnauthorized,
	} {
		badTestCodeUrl := NewTestURLRequest(fmt.Sprintf("%s/status/%d", srv.URL, code))
		rm := requesthelper.GetRequestManager(badTestCodeUrl, testRequestTimeout)
		_, err := rm.MakeRequest(ctx)
		require.Equal(t, badTestCodeUrl.calls, 1)
		require.NotNil(t, err, "not failed for code: %d", code)
		require.Contains(t, err.Error(), "unexpected status code", "actual=%d", code)
	}
}

func TestMakeRequest_MultipleStatusOKSucceeds(t *testing.T) {
	ctx := log.NewContext(log.NewSyncLogger(log.NewLogfmtLogger(os.Stdout))).With("time", log.DefaultTimestamp)
	srv := httptest.NewServer(httpbin.GetMux())
	defer srv.Close()

	okTestUrl := NewTestURLRequest(srv.URL + "/status/200")
	rm := requesthelper.GetRequestManager(okTestUrl, testRequestTimeout)
	for totalCalls := 1; totalCalls <= 5; totalCalls++ {
		resp, err := rm.MakeRequest(ctx)
		require.Equal(t, okTestUrl.calls, totalCalls)
		require.Nil(t, err)
		defer resp.Body.Close()
		require.NotNil(t, resp.Body)
	}
}

func TestMakeRequest_StatusPartialGetResultsSucceeds(t *testing.T) {
	ctx := log.NewContext(log.NewSyncLogger(log.NewLogfmtLogger(os.Stdout))).With("time", log.DefaultTimestamp)
	srv := httptest.NewServer(httpbin.GetMux())
	defer srv.Close()

	partialTestGetRequest := NewTestURLRequest(srv.URL + "/status/206")
	rm := requesthelper.GetRequestManager(partialTestGetRequest, testRequestTimeout)
	resp, err := rm.MakeRequest(ctx)
	require.Equal(t, partialTestGetRequest.calls, 1)
	require.Nil(t, err)
	defer resp.Body.Close()
	require.NotNil(t, resp.Body)
}

func TestMakeRequest_RetrievesBody(t *testing.T) {
	ctx := log.NewContext(log.NewSyncLogger(log.NewLogfmtLogger(os.Stdout))).With("time", log.DefaultTimestamp)
	srv := httptest.NewServer(httpbin.GetMux())
	defer srv.Close()

	testRequest := NewTestURLRequest(srv.URL + "/bytes/65536")
	rm := requesthelper.GetRequestManager(testRequest, testRequestTimeout)
	resp, err := rm.MakeRequest(ctx)
	require.Equal(t, testRequest.calls, 1)
	require.Nil(t, err)
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	require.Nil(t, err)
	require.EqualValues(t, 65536, len(b))
}

func TestMakeRequest_BodyClosesWithoutError(t *testing.T) {
	ctx := log.NewContext(log.NewSyncLogger(log.NewLogfmtLogger(os.Stdout))).With("time", log.DefaultTimestamp)
	srv := httptest.NewServer(httpbin.GetMux())
	defer srv.Close()

	testRequest := NewTestURLRequest(srv.URL + "/get")
	rm := requesthelper.GetRequestManager(testRequest, testRequestTimeout)
	resp, err := rm.MakeRequest(ctx)
	require.Nil(t, err)
	require.Nil(t, resp.Body.Close())
}
