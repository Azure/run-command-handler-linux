package hostgacommunicator

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"testing"

	"github.com/Azure/run-command-handler-linux/internal/constants"
	"github.com/Azure/run-command-handler-linux/internal/requesthelper"
	"github.com/go-kit/kit/log"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
)

func Test_GetOperationUri(t *testing.T) {
	ctx := log.NewContext(log.NewSyncLogger(log.NewLogfmtLogger(os.Stdout))).With("time", log.DefaultTimestamp)
	operationName := "testOperationName"
	uri, err := getOperationUri(ctx, operationName)
	require.Nil(t, err)
	require.NotNil(t, uri)
	require.Contains(t, uri, operationName)
}

type fakeVMSettingsRequestManager struct {
	rm  *requesthelper.RequestManager
	err error
}

func (f fakeVMSettingsRequestManager) GetVMSettingsRequestManager(ctx *log.Context) (*requesthelper.RequestManager, error) {
	return f.rm, f.err
}

func TestGetImmediateVMSettings_RequestManagerError(t *testing.T) {
	orig := withRetriesFn
	t.Cleanup(func() { withRetriesFn = orig })

	// withRetries should never be called in this branch
	withRetriesFn = func(_ *log.Context, _ *requesthelper.RequestManager, _ requesthelper.SleepFunc, _ string) (*http.Response, error) {
		t.Fatalf("withRetriesFn should not have been called")
		return nil, nil
	}

	rmErr := errors.New("the chipmunks have new management")
	c := NewHostGACommunicator(fakeVMSettingsRequestManager{rm: nil, err: rmErr})

	_, err := c.GetImmediateVMSettings(nil, "etag0")
	VerifyErrorClarification(t, constants.Internal_UnknownError, err)
}

func TestGetImmediateVMSettings_WithRetriesError_WrappedWithClarification(t *testing.T) {
	orig := withRetriesFn
	t.Cleanup(func() { withRetriesFn = orig })

	withRetriesFn = func(_ *log.Context, _ *requesthelper.RequestManager, _ requesthelper.SleepFunc, _ string) (*http.Response, error) {
		return nil, errors.New("network fail")
	}

	c := NewHostGACommunicator(fakeVMSettingsRequestManager{rm: &requesthelper.RequestManager{}, err: nil})

	_, err := c.GetImmediateVMSettings(nil, "etag0")
	VerifyErrorClarification(t, constants.Internal_UnknownError, err)
}

func TestGetImmediateVMSettings_NotModified304_ReturnsUnmodifiedResponse(t *testing.T) {
	orig := withRetriesFn
	t.Cleanup(func() { withRetriesFn = orig })

	withRetriesFn = func(_ *log.Context, _ *requesthelper.RequestManager, _ requesthelper.SleepFunc, _ string) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusNotModified,
			Body:       io.NopCloser(bytes.NewReader(nil)),
			Header:     make(http.Header),
		}, nil
	}

	c := NewHostGACommunicator(fakeVMSettingsRequestManager{rm: &requesthelper.RequestManager{}, err: nil})

	resp, err := c.GetImmediateVMSettings(nil, "etag0")
	require.Nil(t, err, "unexpected err: %v", err)
	require.Nil(t, resp.VMSettings, "expected VMSettings nil")
	require.Equal(t, "etag0", resp.ETag, "expected ETag preserved, got %q", resp.ETag)
	require.False(t, resp.Modified, "expected Modified=false")
}

func TestGetImmediateVMSettings_NotFound404_ReturnsUnmodifiedResponse(t *testing.T) {
	orig := withRetriesFn
	t.Cleanup(func() { withRetriesFn = orig })

	withRetriesFn = func(_ *log.Context, _ *requesthelper.RequestManager, _ requesthelper.SleepFunc, _ string) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusNotFound,
			Body:       io.NopCloser(bytes.NewReader(nil)),
			Header:     make(http.Header),
		}, nil
	}

	c := NewHostGACommunicator(fakeVMSettingsRequestManager{rm: &requesthelper.RequestManager{}, err: nil})

	resp, err := c.GetImmediateVMSettings(nil, "etag0")
	require.Nil(t, err, "unexpected err: %v", err)
	require.Nil(t, resp.VMSettings, "expected VMSettings nil")
	require.Equal(t, "etag0", resp.ETag, "expected ETag preserved, got %q", resp.ETag)
	require.False(t, resp.Modified, "expected Modified=false")
}

func TestGetImmediateVMSettings_BadJSON_ReturnsFailedToParseSettings(t *testing.T) {
	orig := withRetriesFn
	t.Cleanup(func() { withRetriesFn = orig })

	withRetriesFn = func(_ *log.Context, _ *requesthelper.RequestManager, _ requesthelper.SleepFunc, _ string) (*http.Response, error) {
		h := make(http.Header)
		h.Set(constants.ETagHeaderName, "etag1") // still present, but parse should fail first
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewReader([]byte("{not-json"))),
			Header:     h,
		}, nil
	}

	c := NewHostGACommunicator(fakeVMSettingsRequestManager{rm: &requesthelper.RequestManager{}, err: nil})

	_, err := c.GetImmediateVMSettings(nil, "etag0")
	VerifyErrorClarification(t, constants.Hgap_FailedToParseImmediateSettings, err)
}

func TestGetImmediateVMSettings_MissingETagHeader_ReturnsEtagNotFoundClarification(t *testing.T) {
	orig := withRetriesFn
	t.Cleanup(func() { withRetriesFn = orig })

	withRetriesFn = func(_ *log.Context, _ *requesthelper.RequestManager, _ requesthelper.SleepFunc, _ string) (*http.Response, error) {
		// minimal valid JSON for VMImmediateExtensionsGoalState; if required fields exist, update accordingly.
		body := []byte(`{}`)
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewReader(body)),
			Header:     make(http.Header), // no ETag set
		}, nil
	}

	c := NewHostGACommunicator(fakeVMSettingsRequestManager{rm: &requesthelper.RequestManager{}, err: nil})

	_, err := c.GetImmediateVMSettings(nil, "etag0")
	VerifyErrorClarification(t, constants.Hgap_EtagNotFound, err)
}

func TestGetImmediateVMSettings_Success_ModifiedFlagAndETagReturned(t *testing.T) {
	orig := withRetriesFn
	t.Cleanup(func() { withRetriesFn = orig })

	withRetriesFn = func(_ *log.Context, _ *requesthelper.RequestManager, _ requesthelper.SleepFunc, _ string) (*http.Response, error) {
		h := make(http.Header)
		h.Set(constants.ETagHeaderName, "etag1")
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewReader([]byte(`{}`))),
			Header:     h,
		}, nil
	}

	c := NewHostGACommunicator(fakeVMSettingsRequestManager{rm: &requesthelper.RequestManager{}, err: nil})

	resp, err := c.GetImmediateVMSettings(nil, "etag0")
	require.Nil(t, err, "unexpected err: %v", err)
	require.NotNil(t, resp.VMSettings, "expected VMSettings non-nil")
	require.Equal(t, "etag1", resp.ETag, "expected etag1 preserved, got %q", resp.ETag)
	require.True(t, resp.Modified, "expected Modified=true when etag changes")
}

func TestGetOperationUri_InvalidFallbackAddress(t *testing.T) {
	orig := WireServerFallbackAddress
	t.Cleanup(func() { WireServerFallbackAddress = orig })

	// This should make url.Parse fail (unclosed IPv6 literal).
	WireServerFallbackAddress = "http://[::1:32526"

	_, err := getOperationUri(nil, "/machine")
	VerifyErrorClarification(t, constants.Hgap_FailedToParseAddress, err)
}
