package servicehandler

import (
	"errors"
	"io"
	"os"
	"testing"

	"github.com/Azure/run-command-handler-linux/internal/constants"
	"github.com/Azure/run-command-handler-linux/pkg/errorutil"
	"github.com/go-kit/kit/log"
	"github.com/stretchr/testify/require"
)

func requireServiceCode(t *testing.T, want errorutil.ErrorClarificationCode, err error) {
	t.Helper()
	require.Error(t, err)
	code, ok := errorutil.Code(err)
	require.True(t, ok, "missing clarification: %v", err)
	require.Equal(t, want, code)
}

func TestServiceHandlerClarifications(t *testing.T) {
	t.Setenv(constants.VersionEnvName, installedTargetVersion)
	ctx := log.NewContext(log.NewLogfmtLogger(io.Discard))
	cause := errors.New("manager failure")
	for _, want := range []int{errorutil.Service_failedToStart, errorutil.Service_failedToStop, errorutil.Service_failedToEnable, errorutil.Service_failedToDisable, errorutil.Service_failedToReloadDaemon, errorutil.Service_failedToCheckActivity, errorutil.Service_failedToCheckEnabled, errorutil.Service_failedToCheckInstalled, errorutil.Service_failedToRemoveOldConfiguration, errorutil.Service_failedToCreateConfiguration, errorutil.Service_failedToUnregister, errorutil.Internal_failedToReadServiceVersion} {
		m := getManagerMock()
		fail := func(string, *log.Context) error { return cause }
		h := NewHandler(m, NewConfiguration("test.service"), ctx)
		var err error
		switch want {
		case errorutil.Service_failedToStart:
			m.start_f = fail
			err = h.Start()
		case errorutil.Service_failedToStop:
			m.stop_f = fail
			err = h.Stop()
		case errorutil.Service_failedToEnable:
			m.enable_f = fail
			err = h.Enable()
		case errorutil.Service_failedToDisable:
			m.disable_f = fail
			err = h.Disable()
		case errorutil.Service_failedToReloadDaemon:
			m.daemondreload_f = fail
			err = h.DaemonReload()
		case errorutil.Service_failedToCheckActivity:
			m.isactive_f = fail
			_, err = h.IsActive()
		case errorutil.Service_failedToCheckEnabled:
			m.isenabled_f = func(string, *log.Context) (bool, error) { return false, cause }
			_, err = h.IsEnabled()
		case errorutil.Service_failedToCheckInstalled:
			m.isinstalled_f = func(string, *log.Context) (bool, error) { return false, cause }
			_, err = h.IsInstalled()
		case errorutil.Service_failedToRemoveOldConfiguration:
			m.removeUnitConfiguration_f = fail
			err = h.Register(ctx, "")
		case errorutil.Service_failedToCreateConfiguration:
			m.createUnitConfiguration_f = func(string, []byte, *log.Context) error { return cause }
			err = h.Register(ctx, "")
		case errorutil.Service_failedToUnregister:
			m.removeUnitConfiguration_f = fail
			err = h.DeRegister(ctx)
		case errorutil.Internal_failedToReadServiceVersion:
			m.getInstalledVersion_f = func(string, *log.Context) (string, error) { return "", cause }
			_, err = h.GetInstalledVersion(ctx)
		}
		code, ok := errorutil.Code(err)
		require.True(t, ok)
		require.Equal(t, want, code)
		require.ErrorIs(t, err, cause)
	}
}

func TestServiceHandlerPreservesLowerClarifications(t *testing.T) {
	ctx := log.NewContext(log.NewLogfmtLogger(io.Discard))
	cause := errors.New("lower failure")
	lower := errorutil.WithCode(-999, cause)
	m := getManagerMock()
	m.createUnitConfiguration_f = func(string, []byte, *log.Context) error { return lower }
	h := NewHandler(m, NewConfiguration("test.service"), ctx)
	err := h.Register(ctx, "")
	code, ok := errorutil.Code(err)
	require.True(t, ok)
	require.Equal(t, -999, code)
	require.ErrorIs(t, err, cause)
	m = getManagerMock()
	m.removeUnitConfiguration_f = func(string, *log.Context) error {
		return errorutil.WithCode(998, &os.PathError{Op: "remove", Path: "test", Err: os.ErrNotExist})
	}
	h = NewHandler(m, NewConfiguration("test.service"), ctx)
	require.NoError(t, h.Register(ctx, ""), "wrapped ENOENT remains ignorable")
}
