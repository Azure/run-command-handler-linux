package systemd

import (
	"errors"
	"io"
	"os"
	"testing"
	"time"

	"github.com/Azure/run-command-handler-linux/pkg/errorutil"
	"github.com/go-kit/kit/log"
	"github.com/stretchr/testify/require"
)

type systemdFileInfo struct{ directory bool }

func (i systemdFileInfo) Name() string       { return "unit" }
func (i systemdFileInfo) Size() int64        { return 0 }
func (i systemdFileInfo) Mode() os.FileMode  { return 0 }
func (i systemdFileInfo) ModTime() time.Time { return time.Time{} }
func (i systemdFileInfo) IsDir() bool        { return i.directory }
func (i systemdFileInfo) Sys() interface{}   { return nil }

func TestSystemdClarifications(t *testing.T) {
	oldRun, oldPath := runSystemctl, GetUnitConfigurationFilePath
	oldStat, oldWrite, oldRemove, oldRead, oldBaseStat := statUnitFile, writeUnitFile, removeUnitFile, readUnitFile, statSystemdPath
	t.Cleanup(func() {
		runSystemctl, GetUnitConfigurationFilePath = oldRun, oldPath
		statUnitFile, writeUnitFile, removeUnitFile, readUnitFile, statSystemdPath = oldStat, oldWrite, oldRemove, oldRead, oldBaseStat
	})
	ctx := log.NewContext(log.NewLogfmtLogger(io.Discard))
	cause := errors.New("systemd failure")
	assertCode := func(err error, want int) {
		t.Helper()
		code, ok := errorutil.Code(err)
		require.True(t, ok, "%v", err)
		require.Equal(t, want, code)
	}
	runSystemctl = func(...string) ([]byte, error) { return nil, cause }
	mgr := NewUnitManager()
	for _, tc := range []struct {
		code int
		run  func(string, *log.Context) error
	}{
		{errorutil.Systemd_failedToStartUnit, mgr.StartUnit}, {errorutil.Systemd_failedToStopUnit, mgr.StopUnit},
		{errorutil.Systemd_failedToEnableUnit, mgr.EnableUnit}, {errorutil.Systemd_failedToDisableUnit, mgr.DisableUnit},
		{errorutil.Systemd_failedToReloadDaemon, mgr.DaemonReload}, {errorutil.Systemd_failedToCheckUnitActivity, mgr.IsUnitActive},
	} {
		err := tc.run("test.service", ctx)
		assertCode(err, tc.code)
		require.ErrorIs(t, err, cause)
	}
	_, err := mgr.IsUnitEnabled("test", ctx)
	assertCode(err, errorutil.Systemd_failedToCheckUnitEnabled)
	for _, state := range []string{"enabled\n", "disabled\n"} {
		runSystemctl = func(...string) ([]byte, error) { return []byte(state), cause }
		enabled, err := mgr.IsUnitEnabled("test", ctx)
		require.NoError(t, err)
		require.Equal(t, state == "enabled\n", enabled)
	}
	GetUnitConfigurationFilePath = func(string, *log.Context) (string, error) { return "test", nil }
	statUnitFile = func(string) (os.FileInfo, error) { return nil, cause }
	_, err = mgr.IsUnitInstalled("test", ctx)
	assertCode(err, errorutil.Systemd_failedToCheckUnitFile)
	writeUnitFile = func(string, []byte, os.FileMode) error { return cause }
	assertCode(mgr.CreateUnitConfigurationFile("test", nil, ctx), errorutil.Systemd_failedToWriteUnitConfiguration)
	removeUnitFile = func(string) error { return cause }
	assertCode(mgr.RemoveUnitConfigurationFile("test", ctx), errorutil.Systemd_failedToRemoveUnitConfiguration)
	readUnitFile = func(string) ([]byte, error) { return nil, cause }
	_, err = mgr.GetInstalledVersion("test", ctx)
	assertCode(err, errorutil.Systemd_failedToReadUnitConfiguration)
	require.ErrorIs(t, err, cause)
	statSystemdPath = func(string) (os.FileInfo, error) { return nil, cause }
	_, err = GetSystemDConfigurationBasePath(ctx)
	assertCode(err, errorutil.Systemd_failedToInspectUnitDirectory)
	require.ErrorIs(t, err, cause)
	statSystemdPath = func(string) (os.FileInfo, error) { return systemdFileInfo{}, nil }
	_, err = GetSystemDConfigurationBasePath(ctx)
	assertCode(err, errorutil.Systemd_noUnitConfigurationDirectory)
}
