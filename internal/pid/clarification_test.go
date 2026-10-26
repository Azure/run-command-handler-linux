package pid

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/Azure/run-command-handler-linux/pkg/errorutil"
	"github.com/go-kit/kit/log"
	"github.com/stretchr/testify/require"
)

type pidErrorLogger struct{ err error }

func (l *pidErrorLogger) Log(values ...interface{}) error {
	for _, value := range values {
		if err, ok := value.(error); ok {
			l.err = err
		}
	}
	return nil
}

func TestPIDClarifications(t *testing.T) {
	oldStart, oldKill := processStartTime, killProcessGroup
	t.Cleanup(func() { processStartTime, killProcessGroup = oldStart, oldKill })
	cause := errors.New("machine failure")
	assertCode := func(err error, want int) {
		t.Helper()
		code, ok := errorutil.Code(err)
		require.True(t, ok, "%v", err)
		require.Equal(t, want, code)
	}
	processStartTime = func(string) ([]byte, error) { return nil, cause }
	_, err := GetProcessStartTime(123)
	assertCode(err, errorutil.Os_FailedToGetProcessStartTime)
	require.ErrorIs(t, err, cause)
	processStartTime = func(string) ([]byte, error) { return []byte("start"), nil }
	dir, err := os.MkdirTemp(".", ".clarification-")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })
	assertCode(SaveCurrentPidAndStartTime(dir), errorutil.Os_FailedToWriteProcessRecord)
	file := filepath.Join(dir, "pid")
	assertCode(DeleteCurrentPidAndStartTime(file), errorutil.Os_FailedToDeleteProcessRecord)
	_, _, err = ReadPidAndStartTime(dir)
	assertCode(err, errorutil.Os_FailedToReadProcessRecord)
	require.NoError(t, os.WriteFile(file, []byte("bad"), 0600))
	_, _, err = ReadPidAndStartTime(file)
	assertCode(err, errorutil.Os_InvalidProcessRecordFormat)
	require.NoError(t, os.WriteFile(file, []byte("bad\tstart"), 0600))
	_, _, err = ReadPidAndStartTime(file)
	assertCode(err, errorutil.Os_InvalidProcessRecordId)
	require.NoError(t, os.WriteFile(file, []byte("123\tstart"), 0600))
	killProcessGroup = func(pid int, signal syscall.Signal) error {
		require.Equal(t, -123, pid)
		require.Equal(t, syscall.SIGKILL, signal)
		return cause
	}
	logger := &pidErrorLogger{}
	KillPreviousExtension(log.NewContext(logger), file)
	assertCode(logger.err, errorutil.Os_FailedToKillPreviousProcessGroup)
	require.ErrorIs(t, logger.err, cause)
}
