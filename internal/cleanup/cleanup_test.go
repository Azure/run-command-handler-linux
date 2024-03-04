package cleanup_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/Azure/run-command-handler-linux/internal/cleanup"
	"github.com/Azure/run-command-handler-linux/internal/types"
	"github.com/go-kit/kit/log"
	"github.com/stretchr/testify/require"
)

func TestRunCommandCleanupSuccess_DeletesAllFilesExceptMostRecent(t *testing.T) {
	ctx := log.NewContext(log.NewSyncLogger(log.NewLogfmtLogger(os.Stdout))).With("time", log.DefaultTimestamp)
	extName, seqNum := "testExtension", 5
	dataDir, err := os.MkdirTemp("", "")
	defer os.RemoveAll(dataDir)
	require.Nil(t, err)
	require.DirExists(t, dataDir)

	downloadFolder, fakeEnv, scriptFilePathsForSeqs, runtimeSettingsForSeqs := createTempScriptsAndSettingsAndGetVariables(t, dataDir, extName, seqNum)

	metadata := types.NewRCMetadata(extName, seqNum, filepath.Base(downloadFolder), dataDir)
	cleanup.RunCommandCleanup(ctx, metadata, fakeEnv, "")
	checkOnlyNotMostRecentSeqFilesWereAffectedForGivenExt(t, scriptFilePathsForSeqs, runtimeSettingsForSeqs)
}

func TestRunCommandCleanupSuccessMultipleExtensions_DeletesAllFilesExceptMostRecentForSelectedExtension(t *testing.T) {
	ctx := log.NewContext(log.NewSyncLogger(log.NewLogfmtLogger(os.Stdout))).With("time", log.DefaultTimestamp)
	extName, seqNum := "testExtension", 5
	dataDir, err := os.MkdirTemp("", "")
	defer os.RemoveAll(dataDir)
	require.Nil(t, err)
	require.DirExists(t, dataDir)

	downloadFolder, fakeEnv, scriptFilePathsForSeqs, runtimeSettingsForSeqs := createTempScriptsAndSettingsAndGetVariables(t, dataDir, extName, seqNum)
	_, _, scriptFilePathsNotRelatedExt, runtimeSettingsNotRelatedExt := createTempScriptsAndSettingsAndGetVariables(t, dataDir, "notRelatedExtension", seqNum)

	metadata := types.NewRCMetadata(extName, seqNum, filepath.Base(downloadFolder), dataDir)
	cleanup.RunCommandCleanup(ctx, metadata, fakeEnv, "")
	checkOnlyNotMostRecentSeqFilesWereAffectedForGivenExt(t, scriptFilePathsForSeqs, runtimeSettingsForSeqs)
	checkNotRelatedExtFilesWereNotAffected(t, scriptFilePathsNotRelatedExt, runtimeSettingsNotRelatedExt)
}

func TestRunCommandCleanupSuccess_DeletesAllFiles(t *testing.T) {
	ctx := log.NewContext(log.NewSyncLogger(log.NewLogfmtLogger(os.Stdout))).With("time", log.DefaultTimestamp)
	extName, seqNum := "testExtension", 5
	dataDir, err := os.MkdirTemp("", "")
	defer os.RemoveAll(dataDir)
	require.Nil(t, err)
	require.DirExists(t, dataDir)

	downloadFolder, fakeEnv, scriptFilePathsForSeqs, runtimeSettingsForSeqs := createTempScriptsAndSettingsAndGetVariables(t, dataDir, extName, seqNum)

	metadata := types.NewRCMetadata(extName, seqNum, filepath.Base(downloadFolder), dataDir)
	cleanup.ImmediateRunCommandCleanup(ctx, metadata, fakeEnv, "")

	// check that all script folders and files where deleted
	for i := 0; i < len(scriptFilePathsForSeqs); i++ {
		require.NoDirExists(t, filepath.Dir(scriptFilePathsForSeqs[i]))
		require.NoFileExists(t, scriptFilePathsForSeqs[i])
	}

	// check that runtime settings were actually deleted
	for i := 0; i < len(runtimeSettingsForSeqs); i++ {
		require.NoFileExists(t, runtimeSettingsForSeqs[i])
	}
}

func TestRunCommandCleanupSuccessMultipleExtensions_DeletesAllFilesForSelectedExtension(t *testing.T) {
	ctx := log.NewContext(log.NewSyncLogger(log.NewLogfmtLogger(os.Stdout))).With("time", log.DefaultTimestamp)
	extName, seqNum := "testExtension", 5
	dataDir, err := os.MkdirTemp("", "")
	defer os.RemoveAll(dataDir)
	require.Nil(t, err)
	require.DirExists(t, dataDir)

	downloadFolder, fakeEnv, scriptFilePathsForSeqs, runtimeSettingsForSeqs := createTempScriptsAndSettingsAndGetVariables(t, dataDir, extName, seqNum)
	_, _, scriptFilePathsNotRelatedExt, runtimeSettingsNotRelatedExt := createTempScriptsAndSettingsAndGetVariables(t, dataDir, "notRelatedExtension", seqNum)

	metadata := types.NewRCMetadata(extName, seqNum, filepath.Base(downloadFolder), dataDir)
	cleanup.ImmediateRunCommandCleanup(ctx, metadata, fakeEnv, "")

	// check that all script folders and files where deleted
	for i := 0; i < len(scriptFilePathsForSeqs); i++ {
		require.NoDirExists(t, filepath.Dir(scriptFilePathsForSeqs[i]))
		require.NoFileExists(t, scriptFilePathsForSeqs[i])
	}

	// check that runtime settings were actually deleted
	for i := 0; i < len(runtimeSettingsForSeqs); i++ {
		require.NoFileExists(t, runtimeSettingsForSeqs[i])
	}
	checkNotRelatedExtFilesWereNotAffected(t, scriptFilePathsNotRelatedExt, runtimeSettingsNotRelatedExt)
}

func createTempScriptsAndSettingsAndGetVariables(t *testing.T, dataDir string, extName string, maxSeqNum int) (string, types.HandlerEnvironment, []string, []string) {
	downloadFolder, err := os.MkdirTemp(dataDir, "download_*")
	require.Nil(t, err)
	require.DirExists(t, downloadFolder)

	fakeEnv := types.HandlerEnvironment{}
	fakeEnv.HandlerEnvironment.ConfigFolder = filepath.Join(dataDir, "config")

	var scriptFilePathsForSeqs []string
	var runtimeSettingsForSeqs []string
	for s := 1; s <= maxSeqNum; s++ {
		scriptFilePathsForSeqs = append(scriptFilePathsForSeqs, createScriptDirForSeqNum(t, downloadFolder, extName, s))
		runtimeSettingsForSeqs = append(runtimeSettingsForSeqs, createRuntimeSettingsForSeqNum(t, fakeEnv.HandlerEnvironment.ConfigFolder, extName, s))
	}

	return downloadFolder, fakeEnv, scriptFilePathsForSeqs, runtimeSettingsForSeqs
}

func createScriptDirForSeqNum(t *testing.T, directory string, extName string, seqNum int) string {
	seqNumString := strconv.Itoa(seqNum)
	dirName := filepath.Join(directory, extName, seqNumString)
	err := os.MkdirAll(dirName, 0700)
	require.DirExists(t, dirName)
	require.Nil(t, err)

	fileNamePattern := seqNumString + "_*.sh"
	file, err := os.CreateTemp(dirName, fileNamePattern)
	require.Nil(t, err)
	require.FileExists(t, file.Name())
	defer file.Close()

	return file.Name()
}

func createRuntimeSettingsForSeqNum(t *testing.T, directory string, extName string, seqNum int) string {
	if _, err := os.Stat(directory); errors.Is(err, os.ErrNotExist) {
		err := os.MkdirAll(directory, 0700)
		require.DirExists(t, directory)
		require.Nil(t, err)
	}

	settingsFilePath := filepath.Join(directory, fmt.Sprintf("%v.%v.settings", extName, seqNum))
	file, err := os.Create(settingsFilePath)
	require.Nil(t, err)
	require.FileExists(t, file.Name())
	defer file.Close()

	_, err = file.WriteString("nonemptytext")
	require.Nil(t, err)

	return file.Name()
}

func checkOnlyNotMostRecentSeqFilesWereAffectedForGivenExt(t *testing.T, scriptFilePathsForSeqs []string, runtimeSettingsForSeqs []string) {
	// check that script folders and files where deleted except most recent seqNum
	for i := 0; i < len(scriptFilePathsForSeqs)-1; i++ {
		require.NoDirExists(t, filepath.Dir(scriptFilePathsForSeqs[i]))
		require.NoFileExists(t, scriptFilePathsForSeqs[i])
	}
	require.DirExists(t, filepath.Dir(scriptFilePathsForSeqs[len(scriptFilePathsForSeqs)-1]))
	require.FileExists(t, scriptFilePathsForSeqs[len(scriptFilePathsForSeqs)-1])

	// check that runtime settings were truncated except the most recent one
	for i := 0; i < len(runtimeSettingsForSeqs); i++ {
		require.FileExists(t, runtimeSettingsForSeqs[i])
		content, err := os.ReadFile(runtimeSettingsForSeqs[i])
		require.Nil(t, err)
		if i < len(runtimeSettingsForSeqs)-1 {
			require.Empty(t, string(content))
		} else {
			require.Equal(t, string(content), "nonemptytext")
		}
	}
}

func checkNotRelatedExtFilesWereNotAffected(t *testing.T, scriptFilePathsNotRelatedExt []string, runtimeSettingsNotRelatedExt []string) {
	// Verify that files from not related extension were not deleted
	for i := 0; i < len(scriptFilePathsNotRelatedExt); i++ {
		require.FileExists(t, scriptFilePathsNotRelatedExt[i])

		// Now verify the file was not truncated
		require.FileExists(t, runtimeSettingsNotRelatedExt[i])
		content, err := os.ReadFile(runtimeSettingsNotRelatedExt[i])
		require.Nil(t, err)
		require.Equal(t, string(content), "nonemptytext")
	}
}
