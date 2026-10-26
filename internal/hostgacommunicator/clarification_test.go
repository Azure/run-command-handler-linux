package hostgacommunicator

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/Azure/run-command-handler-linux/internal/requesthelper"
	"github.com/Azure/run-command-handler-linux/internal/settings"
	"github.com/Azure/run-command-handler-linux/internal/types"
	"github.com/Azure/run-command-handler-linux/pkg/errorutil"
	"github.com/go-kit/kit/log"
	"github.com/stretchr/testify/require"
)

type clarificationRequestManager struct {
	manager *requesthelper.RequestManager
	err     error
}

func (m clarificationRequestManager) GetVMSettingsRequestManager(*log.Context) (*requesthelper.RequestManager, error) {
	return m.manager, m.err
}

func TestHostGAClarifications(t *testing.T) {
	ctx := log.NewContext(log.NewLogfmtLogger(io.Discard))
	cause := errors.New("host failure")
	assertCode := func(err error, want int) {
		t.Helper()
		code, ok := errorutil.Code(err)
		require.True(t, ok, "%v", err)
		require.Equal(t, want, code)
	}
	c := NewHostGACommunicator(clarificationRequestManager{err: cause})
	_, err := c.GetImmediateVMSettings(ctx, "")
	assertCode(err, errorutil.HostGA_failedToCreateRequestManager)
	require.ErrorIs(t, err, cause)
	for _, want := range []int{errorutil.HostGA_failedToReadVmSettingsResponse, errorutil.HostGA_failedToParseVmSettingsResponse, errorutil.HostGA_missingVmSettingsETag} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch want {
			case errorutil.HostGA_failedToReadVmSettingsResponse:
				w.Header().Set("Content-Length", "100")
				io.WriteString(w, "{}")
			case errorutil.HostGA_failedToParseVmSettingsResponse:
				io.WriteString(w, "{")
			case errorutil.HostGA_missingVmSettingsETag:
				io.WriteString(w, "{}")
			}
		}))
		manager := requesthelper.GetRequestManager(requestFactory{url: server.URL}, time.Second)
		c := NewHostGACommunicator(clarificationRequestManager{manager: manager})
		_, err := c.GetImmediateVMSettings(ctx, "")
		server.Close()
		assertCode(err, want)
	}
	// errorutil.Internal_invalidWireServerFallbackAddress cannot be triggered: WireServerFallbackAddress is a valid URL constant.
	_, err = (requestFactory{url: "://"}).GetRequest(ctx, "")
	assertCode(err, errorutil.HostGA_failedToCreateVmSettings)
	oldEnv, oldStat := readHandlerEnv, statCertificate
	t.Cleanup(func() { readHandlerEnv, statCertificate = oldEnv, oldStat })
	readHandlerEnv = func() (types.HandlerEnvironment, error) { return types.HandlerEnvironment{}, cause }
	state := ImmediateExtensionGoalState{}
	_, err = state.ValidateSignature()
	assertCode(err, errorutil.HostGA_failedToParseHandlerEnvironment)
	require.ErrorIs(t, err, cause)
	readHandlerEnv = func() (types.HandlerEnvironment, error) { return types.HandlerEnvironment{}, nil }
	state.Settings = []settings.SettingsCommon{{ProtectedSettingsBase64: "protected"}}
	_, err = state.ValidateSignature()
	assertCode(err, errorutil.HostGA_missingSettingsCertificateThumbprint)
	state.Settings[0].SettingsCertThumbprint = "thumbprint"
	statCertificate = func(string) (os.FileInfo, error) { return nil, cause }
	_, err = state.ValidateSignature()
	assertCode(err, errorutil.HostGA_failedToDecryptProtectedSettings)
	require.ErrorIs(t, err, cause)
}
