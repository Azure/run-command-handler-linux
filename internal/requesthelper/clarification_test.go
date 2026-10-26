package requesthelper

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Azure/run-command-handler-linux/pkg/errorutil"
	"github.com/go-kit/kit/log"
	"github.com/stretchr/testify/require"
)

type clarificationFactory struct{ err error }

func (f clarificationFactory) GetRequest(*log.Context, string) (*http.Request, error) {
	if f.err != nil {
		return nil, f.err
	}
	return http.NewRequest(http.MethodGet, "http://example.invalid", nil)
}

type clarificationTransport func(*http.Request) (*http.Response, error)

func (f clarificationTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type temporaryClarificationError struct{}

func (temporaryClarificationError) Error() string   { return "temporary failure" }
func (temporaryClarificationError) Temporary() bool { return true }
func (temporaryClarificationError) Timeout() bool   { return true }

func TestRequestClarifications(t *testing.T) {
	ctx := log.NewContext(log.NewLogfmtLogger(io.Discard))
	cause := errors.New("network failure")
	for _, want := range []int{errorutil.HostGA_failedToCreateRequest, errorutil.HostGA_failedToSendRequest, errorutil.HostGA_requestReturnedErrorStatus} {
		factory := clarificationFactory{}
		if want == errorutil.HostGA_failedToCreateRequest {
			factory.err = cause
		}
		rm := GetRequestManager(factory, time.Second)
		rm.httpClient.Transport = clarificationTransport(func(*http.Request) (*http.Response, error) {
			if want == errorutil.HostGA_failedToSendRequest {
				return nil, cause
			}
			return &http.Response{StatusCode: http.StatusBadRequest, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(""))}, nil
		})
		resp, err := rm.MakeRequest(ctx, "")
		if resp != nil {
			resp.Body.Close()
		}
		code, ok := errorutil.Code(err)
		require.True(t, ok)
		require.Equal(t, want, code)
		if want != errorutil.HostGA_requestReturnedErrorStatus {
			require.ErrorIs(t, err, cause)
		}
	}
}

func TestRequestRetriesThroughClarification(t *testing.T) {
	ctx := log.NewContext(log.NewLogfmtLogger(io.Discard))
	rm := GetRequestManager(clarificationFactory{}, time.Second)
	attempts, sleeps := 0, 0
	rm.httpClient.Transport = clarificationTransport(func(*http.Request) (*http.Response, error) {
		attempts++
		return nil, temporaryClarificationError{}
	})
	_, err := WithRetries(ctx, rm, func(time.Duration) { sleeps++ }, "")
	code, ok := errorutil.Code(err)
	require.True(t, ok)
	require.Equal(t, errorutil.HostGA_failedToSendRequest, code)
	require.Equal(t, expRetryN, attempts)
	require.Equal(t, expRetryN-1, sleeps)
	var temporary interface{ Temporary() bool }
	require.ErrorAs(t, err, &temporary)
	require.True(t, temporary.Temporary())
}
