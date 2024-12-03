package requesthelper_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/Azure/run-command-handler-linux/internal/requesthelper"
	"github.com/ahmetalpbalkan/go-httpbin"
	"github.com/go-kit/kit/log"
	"github.com/stretchr/testify/require"
)

var (
	// how much we sleep between retries
	sleepSchedule = []time.Duration{
		3 * time.Second,
		6 * time.Second,
		12 * time.Second,
		24 * time.Second,
		48 * time.Second,
		96 * time.Second,
	}
)

func TestActualSleep_actuallySleeps(t *testing.T) {
	s := time.Now()
	requesthelper.ActualSleep(time.Second)
	e := time.Since(s)
	require.InEpsilon(t, 1.0, e.Seconds(), 0.05, "took=%fs", e.Seconds())
}

func TestWithRetries_noRetries(t *testing.T) {
	ctx := log.NewContext(log.NewSyncLogger(log.NewLogfmtLogger(os.Stdout))).With("time", log.DefaultTimestamp)
	srv := httptest.NewServer(httpbin.GetMux())
	defer srv.Close()

	d := NewTestURLRequest(srv.URL + "/status/200")
	rm := requesthelper.GetRequestManager(d, testRequestTimeout)

	sr := new(sleepRecorder)
	resp, err := requesthelper.WithRetries(ctx, rm, sr.Sleep, "")
	require.Nil(t, err, "should not fail")
	defer resp.Body.Close()
	require.NotNil(t, resp.Body, "response body exists")
	require.Equal(t, []time.Duration(nil), []time.Duration(*sr), "sleep should not be called")
}

func TestWithRetries_noRecovery(t *testing.T) {
	ctx := log.NewContext(log.NewSyncLogger(log.NewLogfmtLogger(os.Stdout))).With("time", log.DefaultTimestamp)
	srv := httptest.NewServer(httpbin.GetMux())
	defer srv.Close()

	d := NewTestURLRequest(srv.URL + "/status/409")
	rm := requesthelper.GetRequestManager(d, testRequestTimeout)

	sr := new(sleepRecorder)
	resp, err := requesthelper.WithRetries(ctx, rm, sr.Sleep, "")
	require.NotNil(t, err, "should have failed")
	require.Nil(t, resp, "response exists")
	require.Equal(t, []time.Duration(nil), []time.Duration(*sr), "sleep should not be called")
}

func TestWithRetries_noResponse(t *testing.T) {
	ctx := log.NewContext(log.NewSyncLogger(log.NewLogfmtLogger(os.Stdout))).With("time", log.DefaultTimestamp)
	srv := httptest.NewServer(httpbin.GetMux())
	defer srv.Close()

	// Change the url to https for an invalid response
	u, _ := url.Parse(srv.URL + "/status/404")
	u.Scheme = "https"

	d := NewTestURLRequest(u.String())
	rm := requesthelper.GetRequestManager(d, testRequestTimeout)

	sr := new(sleepRecorder)
	resp, err := requesthelper.WithRetries(ctx, rm, sr.Sleep, "")
	require.NotNil(t, err, "should have failed")
	require.Nil(t, resp, "response exists")
	require.Equal(t, []time.Duration(nil), []time.Duration(*sr), "sleep should not be called")
}

func TestWithRetries_failing_validateNumberOfCalls(t *testing.T) {
	ctx := log.NewContext(log.NewSyncLogger(log.NewLogfmtLogger(os.Stdout))).With("time", log.DefaultTimestamp)
	srv := httptest.NewServer(httpbin.GetMux())
	defer srv.Close()

	d := NewTestURLRequest(srv.URL + "/status/429")
	rm := requesthelper.GetRequestManager(d, testRequestTimeout)

	sr := new(sleepRecorder)
	_, err := requesthelper.WithRetries(ctx, rm, sr.Sleep, "")
	require.EqualError(t, err, "unexpected status code: actual=429 expected=200")
	require.EqualValues(t, 7, d.calls, "calls exactly expRetryN times")
}

func TestWithRetries_failedCreateRequest(t *testing.T) {
	ctx := log.NewContext(log.NewSyncLogger(log.NewLogfmtLogger(os.Stdout))).With("time", log.DefaultTimestamp)
	bd := &BadRequestor{}
	rm := requesthelper.GetRequestManager(bd, testRequestTimeout)

	sr := new(sleepRecorder)
	_, err := requesthelper.WithRetries(ctx, rm, sr.Sleep, "")
	require.EqualError(t, err, badRequestorErrorMsg)
	require.EqualValues(t, 1, bd.calls, "called exactly one time")
}

func TestWithRetries_requestFailedTimeout(t *testing.T) {
	ctx := log.NewContext(log.NewSyncLogger(log.NewLogfmtLogger(os.Stdout))).With("time", log.DefaultTimestamp)
	er := NewErrorRequest(false, true)
	rm := requesthelper.GetRequestManager(er, testRequestTimeout)

	sr := new(sleepRecorder)
	_, err := requesthelper.WithRetries(ctx, rm, sr.Sleep, "")
	require.EqualError(t, err, requestErrorMsg)
	require.EqualValues(t, 7, er.calls, "calls exactly expRetryN times")
}

func TestWithRetries_requestFailedTemporary(t *testing.T) {
	ctx := log.NewContext(log.NewSyncLogger(log.NewLogfmtLogger(os.Stdout))).With("time", log.DefaultTimestamp)
	er := NewErrorRequest(true, false)
	rm := requesthelper.GetRequestManager(er, testRequestTimeout)

	sr := new(sleepRecorder)
	_, err := requesthelper.WithRetries(ctx, rm, sr.Sleep, "")
	require.EqualError(t, err, requestErrorMsg)
	require.EqualValues(t, 7, er.calls, "calls exactly expRetryN times")
}

func TestWithRetries_requestFailedOther(t *testing.T) {
	ctx := log.NewContext(log.NewSyncLogger(log.NewLogfmtLogger(os.Stdout))).With("time", log.DefaultTimestamp)
	er := NewErrorRequest(false, false)
	rm := requesthelper.GetRequestManager(er, testRequestTimeout)

	sr := new(sleepRecorder)
	_, err := requesthelper.WithRetries(ctx, rm, sr.Sleep, "")
	require.EqualError(t, err, requestErrorMsg)
	require.EqualValues(t, 1, er.calls, "called exactly one time")
}

func TestWithRetries_failingBadStatusCode_validateSleeps(t *testing.T) {
	ctx := log.NewContext(log.NewSyncLogger(log.NewLogfmtLogger(os.Stdout))).With("time", log.DefaultTimestamp)
	srv := httptest.NewServer(httpbin.GetMux())
	defer srv.Close()

	d := NewTestURLRequest(srv.URL + "/status/429")
	rm := requesthelper.GetRequestManager(d, testRequestTimeout)

	sr := new(sleepRecorder)
	_, err := requesthelper.WithRetries(ctx, rm, sr.Sleep, "")
	require.EqualError(t, err, "unexpected status code: actual=429 expected=200")
	require.Equal(t, sleepSchedule, []time.Duration(*sr))
}

func TestWithRetries_healingServer(t *testing.T) {
	ctx := log.NewContext(log.NewSyncLogger(log.NewLogfmtLogger(os.Stdout))).With("time", log.DefaultTimestamp)
	srv := httptest.NewServer(new(healingServer))
	defer srv.Close()

	d := NewTestURLRequest(srv.URL)
	rm := requesthelper.GetRequestManager(d, testRequestTimeout)
	sr := new(sleepRecorder)
	resp, err := requesthelper.WithRetries(ctx, rm, sr.Sleep, "")
	require.Nil(t, err, "should eventually succeed")
	defer resp.Body.Close()
	require.NotNil(t, resp.Body, "response body exists")

	require.Equal(t, sleepSchedule[:3], []time.Duration(*sr))
}

func NoSleep(d time.Duration) {}

// sleepRecorder keeps track of the durations of Sleep calls
type sleepRecorder []time.Duration

// Sleep does not actually sleep. It records the duration and returns.
func (s *sleepRecorder) Sleep(d time.Duration) {
	*s = append(*s, d)
}

// healingServer returns HTTP 500 until 4th call, then HTTP 200 afterwards
type healingServer int

func (h *healingServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	*h++
	if *h < 4 {
		w.WriteHeader(http.StatusInternalServerError)
	} else {
		w.WriteHeader(http.StatusOK)
	}
}
