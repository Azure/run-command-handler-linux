package service

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/Azure/azure-extension-platform/pkg/extensionevents"
	"github.com/Azure/run-command-handler-linux/pkg/errorutil"
	"github.com/Azure/run-command-handler-linux/pkg/systemd"
	"github.com/go-kit/kit/log"
	"github.com/stretchr/testify/require"
)

func TestServiceClarifications(t *testing.T) {
	oldPresent, oldChmod, oldReport := systemdPresent, markExecutable, reportServiceError
	oldPath := systemd.GetUnitConfigurationFilePath
	t.Cleanup(func() {
		systemdPresent, markExecutable, reportServiceError = oldPresent, oldChmod, oldReport
		systemd.GetUnitConfigurationFilePath = oldPath
	})
	ctx := log.NewContext(log.NewLogfmtLogger(io.Discard))
	reportServiceError = func(*extensionevents.ExtensionEventManager, string, string) {}
	systemdPresent = func() bool { return false }
	err := Register(ctx, nil)
	code, ok := errorutil.Code(err)
	require.True(t, ok)
	require.Equal(t, errorutil.Os_SystemdNotSupported, code)
	dir, err := os.MkdirTemp(".", ".clarification-")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })
	systemd.GetUnitConfigurationFilePath = func(string, *log.Context) (string, error) { return filepath.Join(dir, "missing"), nil }
	systemdPresent = func() bool { return true }
	cause := errors.New("permission denied")
	markExecutable = func(string, os.FileMode) error { return cause }
	err = Register(ctx, nil)
	code, ok = errorutil.Code(err)
	require.True(t, ok)
	require.Equal(t, errorutil.Os_FailedToMakeServiceBinaryExecutable, code)
	require.ErrorIs(t, err, cause)
}
