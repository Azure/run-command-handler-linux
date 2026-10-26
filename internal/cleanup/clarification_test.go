package cleanup

import (
	"errors"
	"io"
	"testing"

	"github.com/Azure/run-command-handler-linux/internal/types"
	"github.com/Azure/run-command-handler-linux/pkg/errorutil"
	"github.com/go-kit/kit/log"
	"github.com/stretchr/testify/require"
)

type cleanupErrorLogger struct{ err error }

type cleanupDirectory struct{}

func (cleanupDirectory) Close() error { return nil }

func (l *cleanupErrorLogger) Log(values ...interface{}) error {
	for _, value := range values {
		if err, ok := value.(error); ok {
			l.err = err
		}
	}
	return nil
}

func TestCleanupClarifications(t *testing.T) {
	oldClear, oldDelete, oldOpen := clearExceptMostRecent, deleteDirectoriesExcept, openDownloadDirectory
	t.Cleanup(func() {
		clearExceptMostRecent, deleteDirectoriesExcept, openDownloadDirectory = oldClear, oldDelete, oldOpen
	})
	cause := errors.New("machine failure")
	for _, want := range []int{errorutil.Os_FailedToCleanOldScriptsAndSettings, errorutil.Os_FailedToOpenDownloadDirectory, errorutil.Os_FailedToCleanOldRunAsDirectories} {
		clearExceptMostRecent = func(*log.Context, string, string, string, uint64, string, string) error {
			if want == errorutil.Os_FailedToCleanOldScriptsAndSettings {
				return cause
			}
			return nil
		}
		deleteDirectoriesExcept = func(*log.Context, string, string) error { return cause }
		openDownloadDirectory = func(string) (io.Closer, error) {
			if want == errorutil.Os_FailedToOpenDownloadDirectory {
				return nil, cause
			}
			return cleanupDirectory{}, nil
		}
		logger := &cleanupErrorLogger{}
		user := ""
		if want == errorutil.Os_FailedToCleanOldRunAsDirectories {
			user = "test"
		}
		RunCommandCleanup(log.NewContext(logger), types.RCMetadata{DownloadPath: "."}, types.HandlerEnvironment{}, user)
		code, ok := errorutil.Code(logger.err)
		require.True(t, ok)
		require.Equal(t, want, code)
		require.ErrorIs(t, logger.err, cause)
	}
}

func TestCleanupPreservesLowerClarification(t *testing.T) {
	oldClear, oldDelete, oldOpen := clearExceptMostRecent, deleteDirectoriesExcept, openDownloadDirectory
	t.Cleanup(func() {
		clearExceptMostRecent, deleteDirectoriesExcept, openDownloadDirectory = oldClear, oldDelete, oldOpen
	})
	cause := errors.New("directory read failed")
	classified := errorutil.WithCode(errorutil.Os_FailedToReadCleanupParentDirectory, cause)
	openDownloadDirectory = func(string) (io.Closer, error) { return cleanupDirectory{}, nil }
	for _, user := range []string{"", "test"} {
		t.Run("runAsUser="+user, func(t *testing.T) {
			clearExceptMostRecent = func(*log.Context, string, string, string, uint64, string, string) error {
				if user == "" {
					return classified
				}
				return nil
			}
			deleteDirectoriesExcept = func(*log.Context, string, string) error { return classified }
			logger := &cleanupErrorLogger{}
			RunCommandCleanup(log.NewContext(logger), types.RCMetadata{DownloadPath: "."}, types.HandlerEnvironment{}, user)
			code, ok := errorutil.Code(logger.err)
			require.True(t, ok)
			require.Equal(t, errorutil.Os_FailedToReadCleanupParentDirectory, code)
			require.ErrorIs(t, logger.err, cause)
		})
	}
}
