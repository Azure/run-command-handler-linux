package linuxutils

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Azure/run-command-handler-linux/pkg/errorutil"
	"github.com/go-kit/kit/log"
	"github.com/stretchr/testify/require"
)

type fakeDirectory struct {
	entries []os.DirEntry
	err     error
}

func (d fakeDirectory) ReadDir(int) ([]os.DirEntry, error) { return d.entries, d.err }
func (d fakeDirectory) Close() error                       { return nil }

type fakeEntry struct {
	directory bool
	name      string
}

func (e fakeEntry) Name() string {
	if e.name != "" {
		return e.name
	}
	return "test"
}

func (e fakeEntry) IsDir() bool                { return e.directory }
func (e fakeEntry) Type() os.FileMode          { return 0 }
func (e fakeEntry) Info() (os.FileInfo, error) { return nil, nil }

type filesystemErrorLogger struct{ err error }

func (l *filesystemErrorLogger) Log(values ...interface{}) error {
	for _, value := range values {
		if err, ok := value.(error); ok {
			l.err = err
		}
	}
	return nil
}

func TestLinuxFilesystemClarifications(t *testing.T) {
	oldOpen, oldRemoveDir, oldRemove, oldTruncate := openDirectory, removeDirectory, removeFile, truncateFile
	t.Cleanup(func() {
		openDirectory, removeDirectory, removeFile, truncateFile = oldOpen, oldRemoveDir, oldRemove, oldTruncate
	})
	cause := errors.New("filesystem failure")
	for _, want := range []int{errorutil.Os_FailedToOpenCleanupParentDirectory, errorutil.Os_FailedToReadCleanupParentDirectory, errorutil.Os_FailedToDeleteScriptDirectory, errorutil.Internal_emptyCleanupFilePattern, errorutil.Os_FailedToOpenSettingsCleanupDirectory, errorutil.Internal_invalidCleanupFilePattern, errorutil.Os_FailedToReadSettingsCleanupDirectory, errorutil.Os_FailedToDeleteSettingsFile, errorutil.Os_FailedToTruncateSettingsFile} {
		t.Run(string(rune(want+5000)), func(t *testing.T) {
			openDirectory = func(string) (directoryReader, error) {
				if want == errorutil.Os_FailedToOpenCleanupParentDirectory || want == errorutil.Os_FailedToOpenSettingsCleanupDirectory {
					return nil, cause
				}
				d := fakeDirectory{entries: []os.DirEntry{fakeEntry{directory: want == errorutil.Os_FailedToDeleteScriptDirectory}}}
				if want == errorutil.Os_FailedToReadCleanupParentDirectory || want == errorutil.Os_FailedToReadSettingsCleanupDirectory {
					d.err = cause
				}
				return d, nil
			}
			removeDirectory = func(string) error { return cause }
			removeFile = func(string) error { return cause }
			truncateFile = func(string, int64) error { return cause }
			logger := &filesystemErrorLogger{}
			ctx := log.NewContext(logger)
			var err error
			if want >= errorutil.Os_FailedToOpenCleanupParentDirectory && want <= errorutil.Os_FailedToDeleteScriptDirectory {
				err = TryDeleteDirectories(ctx, "test")
			} else {
				pattern := "test"
				if want == errorutil.Internal_emptyCleanupFilePattern {
					pattern = ""
				} else if want == errorutil.Internal_invalidCleanupFilePattern {
					pattern = "["
				}
				err = tryClearRegexMatchingFiles(ctx, "test", pattern, want != errorutil.Os_FailedToTruncateSettingsFile)
			}
			if want == errorutil.Os_FailedToDeleteScriptDirectory || want == errorutil.Os_FailedToDeleteSettingsFile || want == errorutil.Os_FailedToTruncateSettingsFile {
				require.NoError(t, err, "cleanup remains best effort")
				err = logger.err
			}
			code, ok := errorutil.Code(err)
			require.True(t, ok, "%v", err)
			require.Equal(t, want, code)
			if want > 0 {
				require.ErrorIs(t, err, cause)
			}
		})
	}
}

func TestCleanupExceptMostRecent(t *testing.T) {
	oldOpen, oldRemoveDir, oldRemove, oldTruncate := openDirectory, removeDirectory, removeFile, truncateFile
	t.Cleanup(func() {
		openDirectory, removeDirectory, removeFile, truncateFile = oldOpen, oldRemoveDir, oldRemove, oldTruncate
	})
	scripts := filepath.Join("downloads", "extension")
	openDirectory = func(name string) (directoryReader, error) {
		require.Contains(t, []string{scripts, "config"}, name)
		if name == scripts {
			return fakeDirectory{entries: []os.DirEntry{
				fakeEntry{name: "1", directory: true},
				fakeEntry{name: "5", directory: true},
				fakeEntry{name: "notes.txt"},
			}}, nil
		}
		return fakeDirectory{entries: []os.DirEntry{
			fakeEntry{name: "extension.1.settings"},
			fakeEntry{name: "extension.5.settings"},
			fakeEntry{name: "other.1.settings"},
		}}, nil
	}
	var deletedDirectories, deletedFiles, truncatedFiles []string
	removeDirectory = func(name string) error {
		deletedDirectories = append(deletedDirectories, name)
		return nil
	}
	removeFile = func(name string) error {
		deletedFiles = append(deletedFiles, name)
		return nil
	}
	truncateFile = func(name string, size int64) error {
		require.Zero(t, size)
		truncatedFiles = append(truncatedFiles, name)
		return nil
	}
	err := TryClearExtensionScriptsDirectoriesAndSettingsFilesExceptMostRecent(
		log.NewContext(log.NewNopLogger()), "downloads", "config", "extension", 5,
		`^extension\.\d+\.settings$`, "extension.%d.settings")
	require.NoError(t, err)
	require.Equal(t, []string{filepath.Join(scripts, "1")}, deletedDirectories)
	require.Equal(t, []string{filepath.Join("config", "extension.1.settings")}, truncatedFiles)
	require.Empty(t, deletedFiles)
}

func TestMostRecentCleanupClarifications(t *testing.T) {
	oldOpen := openDirectory
	t.Cleanup(func() { openDirectory = oldOpen })
	cause := errors.New("filesystem failure")
	for _, test := range []struct {
		name      string
		directory string
		readError bool
		pattern   string
		want      errorutil.ErrorClarificationCode
	}{
		{"open scripts", "downloads", false, "settings", errorutil.Os_FailedToOpenCleanupParentDirectory},
		{"read scripts", "downloads", true, "settings", errorutil.Os_FailedToReadCleanupParentDirectory},
		{"open settings", "config", false, "settings", errorutil.Os_FailedToOpenSettingsCleanupDirectory},
		{"read settings", "config", true, "settings", errorutil.Os_FailedToReadSettingsCleanupDirectory},
		{"empty pattern", "", false, "", errorutil.Internal_emptyCleanupFilePattern},
		{"invalid pattern", "", false, "[", errorutil.Internal_invalidCleanupFilePattern},
	} {
		t.Run(test.name, func(t *testing.T) {
			openDirectory = func(name string) (directoryReader, error) {
				if name == test.directory {
					if test.readError {
						return fakeDirectory{err: cause}, nil
					}
					return nil, cause
				}
				return fakeDirectory{}, nil
			}
			err := TryClearExtensionScriptsDirectoriesAndSettingsFilesExceptMostRecent(
				log.NewContext(log.NewNopLogger()), "downloads", "config", "", 5, test.pattern, "%d.settings")
			code, ok := errorutil.Code(err)
			require.True(t, ok)
			require.Equal(t, test.want, code)
			if test.directory != "" {
				require.ErrorIs(t, err, cause)
			}
		})
	}
}
