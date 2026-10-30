package statusreporter

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Azure/run-command-handler-linux/pkg/errorutil"
	"github.com/go-kit/kit/log"
	"github.com/stretchr/testify/require"
)

type statusTransport func(*http.Request) (*http.Response, error)

func (f statusTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestStatusReporterClarifications(t *testing.T) {
	oldClient := statusHTTPClient
	t.Cleanup(func() { statusHTTPClient = oldClient })
	ctx := log.NewContext(log.NewLogfmtLogger(io.Discard))
	cause := errors.New("transport failure")
	for _, want := range []int{errorutil.HostGA_failedToCreateStatusRequest, errorutil.HostGA_failedToSendStatusRequest, errorutil.HostGA_statusUploadReturnedErrorStatus, errorutil.Internal_emptyStatusUpload} {
		statusHTTPClient = &http.Client{Transport: statusTransport(func(*http.Request) (*http.Response, error) {
			if want == errorutil.HostGA_failedToSendStatusRequest {
				return nil, cause
			}
			return &http.Response{StatusCode: http.StatusForbidden, Status: "403 Forbidden", Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}, nil
		})}
		endpoint, content := "http://example.invalid", "status"
		if want == errorutil.HostGA_failedToCreateStatusRequest {
			endpoint = "://"
		}
		var resp *http.Response
		var err error
		if want == errorutil.Internal_emptyStatusUpload {
			resp, err = NewGuestInformationServiceClient(endpoint).ReportStatus(ctx, "")
		} else {
			resp, err = ReportStatus(ctx, endpoint, content)
		}
		if resp != nil {
			resp.Body.Close()
		}
		code, ok := errorutil.Code(err)
		require.True(t, ok, "%v", err)
		require.Equal(t, want, code)
		if want == errorutil.HostGA_failedToSendStatusRequest {
			require.ErrorIs(t, err, cause)
		} else if want == errorutil.HostGA_statusUploadReturnedErrorStatus {
			require.Contains(t, err.Error(), "403 Forbidden")
		}
	}
	statusHTTPClient = &http.Client{Transport: statusTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}, nil
	})}
	resp, err := ReportStatus(ctx, "http://example.invalid", "status")
	require.ErrorContains(t, err, "404 Not Found")
	code, ok := errorutil.Code(err)
	require.True(t, ok)
	require.Equal(t, errorutil.HostGA_statusUploadReturnedErrorStatus, code)
	resp.Body.Close()
	resp, err = ReportStatus(ctx, "://", "")
	require.NoError(t, err)
	require.Nil(t, resp)
}
