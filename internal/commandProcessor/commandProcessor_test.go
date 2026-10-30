package commandProcessor

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/Azure/run-command-handler-linux/internal/cleanup"
	"github.com/Azure/run-command-handler-linux/internal/constants"
	"github.com/Azure/run-command-handler-linux/internal/handlersettings"
	"github.com/Azure/run-command-handler-linux/internal/settings"
	"github.com/Azure/run-command-handler-linux/internal/status"
	"github.com/Azure/run-command-handler-linux/internal/types"
	"github.com/Azure/run-command-handler-linux/pkg/errorutil"
	"github.com/go-kit/kit/log"
	"github.com/stretchr/testify/require"
)

func enablePreSuccess(ctx *log.Context, h types.HandlerEnvironment, metadata types.RCMetadata, c types.Cmd) error {
	return nil
}
func enablePreThrowError(ctx *log.Context, h types.HandlerEnvironment, metadata types.RCMetadata, c types.Cmd) error {
	return errors.New("expected error")
}

func Test_InitializeLogger(t *testing.T) {
	cmd := types.CmdEnableTemplate.InitializeFunctions(types.CmdFunctions{Invoke: nil, Pre: nil, ReportStatus: status.ReportStatusToLocalFile, Cleanup: cleanup.RunCommandCleanup})
	initializeLogger(cmd)
}

func Test_ExecutePreStepsNilPreFunction(t *testing.T) {
	cmd := types.CmdEnableTemplate.InitializeFunctions(types.CmdFunctions{Invoke: nil, Pre: nil, ReportStatus: status.ReportStatusToLocalFile, Cleanup: cleanup.RunCommandCleanup})
	ctx := initializeLogger(cmd)
	extName, seqNum := "testExtension", 5
	fakeEnv := types.HandlerEnvironment{}

	err := executePreSteps(ctx, cmd, fakeEnv, extName, seqNum, constants.DownloadFolder)
	require.Nil(t, err)
}

func Test_ExecutePreSteps(t *testing.T) {
	cmd := types.CmdEnableTemplate.InitializeFunctions(types.CmdFunctions{Invoke: nil, Pre: enablePreSuccess, ReportStatus: status.ReportStatusToLocalFile, Cleanup: cleanup.RunCommandCleanup})
	ctx := initializeLogger(cmd)
	extName, seqNum := "testExtension", 5
	fakeEnv := types.HandlerEnvironment{}

	err := executePreSteps(ctx, cmd, fakeEnv, extName, seqNum, constants.DownloadFolder)
	require.Nil(t, err)
}

func Test_ExecutePreStepsAndFailed(t *testing.T) {
	cmd := types.CmdEnableTemplate.InitializeFunctions(types.CmdFunctions{Invoke: nil, Pre: enablePreThrowError, ReportStatus: status.ReportStatusToLocalFile, Cleanup: cleanup.RunCommandCleanup})
	ctx := initializeLogger(cmd)
	extName, seqNum := "testExtension", 5
	fakeEnv := types.HandlerEnvironment{}
	fakeEnv.HandlerEnvironment.StatusFolder = t.TempDir()

	err := executePreSteps(ctx, cmd, fakeEnv, extName, seqNum, constants.DownloadFolder)
	require.ErrorContains(t, err, "pre-check step failed")
	requireProcessorCode(t, err, errorutil.Internal_unclassifiedPrecheckFailure)
	require.ErrorContains(t, err, "expected error")
	payload, err := os.ReadFile(filepath.Join(fakeEnv.HandlerEnvironment.StatusFolder, "testExtension.5.status"))
	require.NoError(t, err)
	var report types.StatusReport
	require.NoError(t, json.Unmarshal(payload, &report))
	require.Len(t, report, 1)
	require.Equal(t, types.StatusError, report[0].Status.Status)
	requireProcessorStatusCode(t, report, errorutil.Internal_unclassifiedPrecheckFailure)
}

func Test_SaveConfigurationFileInConfigFolderSuccessfully(t *testing.T) {
	ctx := log.NewContext(log.NewSyncLogger(log.NewLogfmtLogger(os.Stdout))).With("time", log.DefaultTimestamp)
	extName, seqNum := "testExtension", 5
	fakeSettings := handlersettings.HandlerSettingsFile{
		RuntimeSettings: []handlersettings.RunTimeSettingsFile{
			{
				HandlerSettings: settings.SettingsCommon{
					PublicSettings: map[string]interface{}{
						"string": "string",
						"int":    5,
					},
					ProtectedSettingsBase64: "protectedsettings",
					SettingsCertThumbprint:  "thumprint",
					SeqNo:                   &seqNum,
					ExtensionName:           &extName,
					ExtensionState:          &extName,
				},
			},
		},
	}

	tmpDir, err := os.MkdirTemp("", "")
	defer os.RemoveAll(tmpDir)
	require.Nil(t, err)

	fakeEnv := types.HandlerEnvironment{}
	fakeEnv.HandlerEnvironment.ConfigFolder = filepath.Join(tmpDir, "config")
	err = os.Mkdir(fakeEnv.HandlerEnvironment.ConfigFolder, 0700)
	require.Nil(t, err)

	err = storeConfigSettingsFileForLocalExecution(ctx, fakeSettings, fakeEnv, extName, seqNum)
	require.Nil(t, err)

	configFilePath := handlersettings.GetConfigFilePath(fakeEnv.HandlerEnvironment.ConfigFolder, seqNum, extName)
	require.FileExists(t, configFilePath)

	content, err := os.ReadFile(configFilePath)
	jsonContent, _ := json.Marshal(fakeSettings)
	require.Nil(t, err)
	require.Equal(t, jsonContent, content)
}

func Test_GetExtNameFromEnvVariable(t *testing.T) {
	ctx := log.NewContext(log.NewSyncLogger(log.NewLogfmtLogger(os.Stdout))).With("time", log.DefaultTimestamp)
	extName := "testExtension"
	os.Setenv(constants.ConfigExtensionNameEnvName, extName)

	actualExtName := GetExtensionName(ctx)
	require.Equal(t, extName, actualExtName)
}

func Test_GetSeqNumFromEnvVariable(t *testing.T) {
	ctx := log.NewContext(log.NewSyncLogger(log.NewLogfmtLogger(os.Stdout))).With("time", log.DefaultTimestamp)
	r := rand.New(rand.NewSource(time.Now().Unix()))
	randSeqNum := r.Intn(1000)
	os.Setenv(constants.ConfigSequenceNumberEnvName, strconv.Itoa(randSeqNum))

	extName := "testExtension"
	fakeEnv := types.HandlerEnvironment{}
	actualSeqNum, err := getSeqNum(&ctx, fakeEnv, extName)
	require.Nil(t, err)
	require.Equal(t, randSeqNum, actualSeqNum)
}

func Test_GetSeqNumFromConfigFile(t *testing.T) {
	ctx := log.NewContext(log.NewSyncLogger(log.NewLogfmtLogger(os.Stdout))).With("time", log.DefaultTimestamp)
	r := rand.New(rand.NewSource(time.Now().Unix()))
	randSeqNum := r.Intn(1000)
	extName := "testExtension"

	tmpDir, err := os.MkdirTemp("", "")
	defer os.RemoveAll(tmpDir)
	require.Nil(t, err)

	fakeEnv := types.HandlerEnvironment{}
	fakeEnv.HandlerEnvironment.ConfigFolder = filepath.Join(tmpDir, "config")
	err = os.Mkdir(fakeEnv.HandlerEnvironment.ConfigFolder, 0700)
	require.Nil(t, err)

	settingsFilePath := path.Join(fakeEnv.HandlerEnvironment.ConfigFolder, fmt.Sprintf("%v.%v.settings", extName, randSeqNum))
	err = os.WriteFile(settingsFilePath, []byte(strconv.Itoa(randSeqNum)), 0700)
	require.Nil(t, err)

	os.Setenv(constants.ConfigSequenceNumberEnvName, "")
	actualSeqNum, err := getSeqNum(&ctx, fakeEnv, extName)
	require.Nil(t, err)
	require.Equal(t, randSeqNum, actualSeqNum)
}

func Test_GetSeqNumFromEnvVariableFailedToParse(t *testing.T) {
	ctx := log.NewContext(log.NewSyncLogger(log.NewLogfmtLogger(os.Stdout))).With("time", log.DefaultTimestamp)
	os.Setenv(constants.ConfigSequenceNumberEnvName, "invalidnumber")

	extName := "testExtension"
	fakeEnv := types.HandlerEnvironment{}
	_, err := getSeqNum(&ctx, fakeEnv, extName)
	require.ErrorContains(t, err, "failed to parse env variable ConfigSequenceNumber")
	requireProcessorCode(t, err, errorutil.Internal_invalidSequenceEnvironmentVariable)

	os.Setenv(constants.ConfigSequenceNumberEnvName, "-")
	_, err = getSeqNum(&ctx, fakeEnv, extName)
	require.ErrorContains(t, err, "failed to parse env variable ConfigSequenceNumber")
	requireProcessorCode(t, err, errorutil.Internal_invalidSequenceEnvironmentVariable)

	os.Setenv(constants.ConfigSequenceNumberEnvName, "+")
	_, err = getSeqNum(&ctx, fakeEnv, extName)
	require.ErrorContains(t, err, "failed to parse env variable ConfigSequenceNumber")
	requireProcessorCode(t, err, errorutil.Internal_invalidSequenceEnvironmentVariable)
}

func Test_GetSeqNumFromFailedToFind(t *testing.T) {
	ctx := log.NewContext(log.NewSyncLogger(log.NewLogfmtLogger(os.Stdout))).With("time", log.DefaultTimestamp)
	extName := "testExtension"

	tmpDir, err := os.MkdirTemp("", "")
	defer os.RemoveAll(tmpDir)
	require.Nil(t, err)

	fakeEnv := types.HandlerEnvironment{}
	fakeEnv.HandlerEnvironment.ConfigFolder = filepath.Join(tmpDir, "config")
	err = os.Mkdir(fakeEnv.HandlerEnvironment.ConfigFolder, 0700)
	require.Nil(t, err)

	os.Setenv(constants.ConfigSequenceNumberEnvName, "")
	actualSeqNum, err := getSeqNum(&ctx, fakeEnv, extName)
	require.Error(t, err)
	requireProcessorCode(t, err, errorutil.Internal_noSequenceConfigurationFiles)
	require.Contains(t, err.Error(), "can't find out seqnum")
	require.Equal(t, 0, actualSeqNum)
}
