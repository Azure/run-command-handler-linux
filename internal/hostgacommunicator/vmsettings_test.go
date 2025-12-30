package hostgacommunicator

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"testing"

	"github.com/Azure/azure-extension-platform/vmextension"
	"github.com/Azure/run-command-handler-linux/internal/constants"
	"github.com/Azure/run-command-handler-linux/internal/requesthelper"
	"github.com/Azure/run-command-handler-linux/internal/settings"
	"github.com/Azure/run-command-handler-linux/internal/types"
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
	require.ErrorContains(t, err, "failed to parse immediate VMSettings json")
	VerifyErrorClarification(t, constants.Hgap_FailedToParseImmediateSettings, err)
}

func TestRequestFactory_GetRequest_InvalidURL_ReturnsErrorWithClarification(t *testing.T) {
	// Intentionally malformed URL to force http.NewRequest to fail.
	f := requestFactory{url: "http://[::1"} // invalid host bracket

	_, err := f.GetRequest(nil, "")
	if err == nil {
		t.Fatalf("expected error, got nil")
	}

	VerifyErrorClarification(t, constants.Hgap_FailedCreateRequest, err)
}

func Test_GetVMSettingsRequestManager_CannotParseUri(t *testing.T) {
	ctx := log.NewContext(log.NewSyncLogger(log.NewLogfmtLogger(os.Stdout))).With("time", log.DefaultTimestamp)

	wsAddress := WireServerFallbackAddress
	defer func() { WireServerFallbackAddress = wsAddress }()
	WireServerFallbackAddress = ":invalid_chipmunk"

	requestManager, err := GetVMSettingsRequestManager(ctx)
	require.Nil(t, requestManager)
	VerifyErrorClarification(t, constants.Hgap_FailedToCreateRequestFactory, err)
}

func Test_GetImmediateVMSettingsHandleNotFound(t *testing.T) {
	ctx := log.NewContext(log.NewSyncLogger(log.NewLogfmtLogger(os.Stdout))).With("time", log.DefaultTimestamp)
	srv := httptest.NewServer(httpbin.GetMux())
	defer srv.Close()

	testRequest := new(TestRequestManager)
	testRequest.testUrlRequest = NewTestUrlRequest(srv.URL + "/status/404") // not found
	communicator := NewHostGACommunicator(testRequest)

	_, err := communicator.GetImmediateVMSettings(ctx, "")
	require.Nil(t, err, "should not return error as this means no new goal states to process")
}

func TestValidateSignature_CertMissingFromGoalState(t *testing.T) {
	thumb := "abc123"
	extensionName := "noncertchipmunk"

	he := types.HandlerEnvironment{
		Version: 1.0,
		Name:    "ExampleExtension",
	}
	he.HandlerEnvironment.ConfigFolder = "blah"

	orig := getHandlerEnvFn
	defer func() { getHandlerEnvFn = orig }()
	getHandlerEnvFn = func() (types.HandlerEnvironment, error) {
		return he, nil
	}

	gs := &ImmediateExtensionGoalState{
		Name: "test",
		Settings: []settings.SettingsCommon{
			{
				ExtensionName:           &extensionName,
				ProtectedSettingsBase64: "not-empty",
				SettingsCertThumbprint:  thumb,
			},
		},
	}

	ok, err := gs.ValidateSignature()
	require.False(t, ok, "Received success when failure expected")
	VerifyErrorClarification(t, constants.Hgap_CertificateMissingFromGoalState, err)
}

func TestValidateSignature_NoCertThumbprint(t *testing.T) {
	extensionName := "noncertchipmunk"

	he := types.HandlerEnvironment{
		Version: 1.0,
		Name:    "ExampleExtension",
	}
	he.HandlerEnvironment.ConfigFolder = "blah"

	orig := getHandlerEnvFn
	defer func() { getHandlerEnvFn = orig }()
	getHandlerEnvFn = func() (types.HandlerEnvironment, error) {
		return he, nil
	}

	gs := &ImmediateExtensionGoalState{
		Name: "test",
		Settings: []settings.SettingsCommon{
			{
				ExtensionName:           &extensionName,
				ProtectedSettingsBase64: "not-empty",
				SettingsCertThumbprint:  "",
			},
		},
	}

	ok, err := gs.ValidateSignature()
	require.False(t, ok, "Received success when failure expected")
	VerifyErrorClarification(t, constants.Hgap_NoCertThumbprint, err)
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

func TestRequestFactory_GetRequest_SetsIfNoneMatchWhenProvided(t *testing.T) {
	f := requestFactory{url: "http://example.com/foo"}

	req, err := f.GetRequest(nil, "etag-123")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if req.Method != "GET" {
		t.Fatalf("expected GET, got %q", req.Method)
	}
	if got := req.Header.Get(constants.IfNoneMatchHeaderName); got != "etag-123" {
		t.Fatalf("expected If-None-Match %q, got %q", "etag-123", got)
	}
}

func TestRequestFactory_GetRequest_DoesNotSetIfNoneMatchWhenEmpty(t *testing.T) {
	f := requestFactory{url: "http://example.com/foo"}

	req, err := f.GetRequest(nil, "")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got := req.Header.Get(constants.IfNoneMatchHeaderName); got != "" {
		t.Fatalf("expected If-None-Match to be empty, got %q", got)
	}
}

func VerifyErrorClarification(t *testing.T, expectedCode int, err error) {
	require.NotNil(t, err, "No error returned when one was expected")
	var ewc vmextension.ErrorWithClarification
	require.True(t, errors.As(err, &ewc), "Error is not of type ErrorWithClarification")
	require.Equal(t, expectedCode, ewc.ErrorCode, "Expected error %d but received %d", expectedCode, ewc.ErrorCode)
}
