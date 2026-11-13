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
	"github.com/go-kit/kit/log"
	"github.com/stretchr/testify/require"
)

func setCleanupTestHandlerEnvironment(t *testing.T) types.HandlerEnvironment {
	t.Helper()
	root := t.TempDir()
	hEnv := types.HandlerEnvironment{}
	hEnv.HandlerEnvironment.ConfigFolder = root
	data, err := json.Marshal([]types.HandlerEnvironment{hEnv})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(root, handlersettings.HandlerEnvFileName), data, 0600))
	oldExecutable := os.Args[0]
	os.Args[0] = filepath.Join(root, "handler")
	t.Cleanup(func() { os.Args[0] = oldExecutable })
	return hEnv
}

func cleanupTestSettings(failurePolicy bool) handlersettings.HandlerSettingsFile {
	return handlersettings.HandlerSettingsFile{
		RuntimeSettings: []handlersettings.RunTimeSettingsFile{{
			HandlerSettings: settings.SettingsCommon{
				PublicSettings: map[string]interface{}{
					"source":                          map[string]interface{}{"script": "echo test"},
					"runAsUser":                       "cleanup-test-user",
					"treatFailureAsDeploymentFailure": failurePolicy,
				},
			},
		}},
	}
}

func Test_ProcessImmediateHandlerCommand_CleanupAfterFinalStatus(t *testing.T) {
	tests := []struct {
		name        string
		scriptFails bool
		policy      bool
		reportFails bool
	}{
		{name: "success"},
		{name: "failure policy disabled", scriptFails: true},
		{name: "failure policy enabled", scriptFails: true, policy: true},
		{name: "success reporting fails", reportFails: true},
		{name: "failure reporting fails", scriptFails: true, policy: true, reportFails: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			hEnv := setCleanupTestHandlerEnvironment(t)
			name, seq := "cleanup-test", 7
			configPath := handlersettings.GetConfigFilePath(hEnv.HandlerEnvironment.ConfigFolder, seq, name)
			expectedMetadata := types.NewRCMetadata(name, seq, constants.ImmediateDownloadFolder, constants.DataDir)
			events := []string{}
			cleanupCalls := 0
			var invokeContext *log.Context
			cmd := types.CmdEnableTemplate.InitializeFunctions(types.CmdFunctions{
				Invoke: func(ctx *log.Context, env types.HandlerEnvironment, _ *types.RunCommandInstanceView, metadata types.RCMetadata, c types.Cmd) (string, string, error, int) {
					invokeContext = ctx
					events = append(events, "invoke")
					require.FileExists(t, configPath)
					c.Functions.Cleanup(ctx, metadata, env, "cleanup-test-user")
					events = append(events, "invoke returned")
					if test.scriptFails {
						return "script output", "script error", errors.New("script failed"), 1
					}
					return "script output", "", nil, 0
				},
				ReportStatus: func(ctx *log.Context, env types.HandlerEnvironment, _ types.RCMetadata, statusType types.StatusType, _ types.Cmd, message string) error {
					if statusType == types.StatusTransitioning {
						events = append(events, "progress")
						return nil
					}
					events = append(events, "final status")
					require.FileExists(t, configPath, "settings must remain available through final reporting")
					cfg, err := handlersettings.GetHandlerSettings(env.HandlerEnvironment.ConfigFolder, name, seq, ctx)
					require.NoError(t, err)
					require.Equal(t, test.policy, cfg.PublicSettings.TreatFailureAsDeploymentFailure)
					expectedStatus := types.StatusSuccess
					if test.scriptFails && test.policy {
						expectedStatus = types.StatusError
					}
					require.Equal(t, expectedStatus, statusType)
					var view types.RunCommandInstanceView
					require.NoError(t, json.Unmarshal([]byte(message), &view))
					require.Equal(t, "script output", view.Output)
					if test.scriptFails {
						require.Equal(t, types.ExecutionState(types.Failed), view.ExecutionState)
						require.Equal(t, 1, view.ExitCode)
						require.Equal(t, "script error", view.Error)
					} else {
						require.Equal(t, types.ExecutionState(types.Succeeded), view.ExecutionState)
						require.Zero(t, view.ExitCode)
					}
					if test.reportFails {
						return errors.New("status reporting failed")
					}
					return nil
				},
				Cleanup: func(ctx *log.Context, metadata types.RCMetadata, env types.HandlerEnvironment, runAsUser string) {
					cleanupCalls++
					require.Same(t, invokeContext, ctx)
					require.Equal(t, expectedMetadata, metadata)
					require.Equal(t, hEnv, env)
					require.Equal(t, "cleanup-test-user", runAsUser)
					events = append(events, "cleanup")
					require.NoError(t, os.Remove(configPath))
				},
			})
			require.NoError(t, ProcessImmediateHandlerCommand(cmd, cleanupTestSettings(test.policy), name, seq))
			require.Equal(t, []string{"progress", "invoke", "invoke returned", "final status", "cleanup"}, events)
			require.Equal(t, 1, cleanupCalls)
			require.NoFileExists(t, configPath)
		})
	}
}

func Test_ProcessImmediateHandlerCommand_DoesNotInventCleanup(t *testing.T) {
	for _, configured := range []bool{false, true} {
		t.Run(fmt.Sprintf("cleanup configured=%t", configured), func(t *testing.T) {
			hEnv := setCleanupTestHandlerEnvironment(t)
			cleanupCalls := 0
			cmd := types.CmdEnableTemplate.InitializeFunctions(types.CmdFunctions{
				Invoke: func(_ *log.Context, _ types.HandlerEnvironment, _ *types.RunCommandInstanceView, _ types.RCMetadata, c types.Cmd) (string, string, error, int) {
					if !configured {
						require.Nil(t, c.Functions.Cleanup)
					}
					return "", "", nil, 0
				},
				ReportStatus: func(_ *log.Context, _ types.HandlerEnvironment, _ types.RCMetadata, _ types.StatusType, _ types.Cmd, _ string) error {
					return nil
				},
			})
			if configured {
				cmd.Functions.Cleanup = func(_ *log.Context, _ types.RCMetadata, _ types.HandlerEnvironment, _ string) {
					cleanupCalls++
				}
			}
			require.NoError(t, ProcessImmediateHandlerCommand(cmd, cleanupTestSettings(false), "cleanup-test", 1))
			require.Zero(t, cleanupCalls, "cleanup must not run unless the command requests it")
			require.FileExists(t, handlersettings.GetConfigFilePath(hEnv.HandlerEnvironment.ConfigFolder, 1, "cleanup-test"))
		})
	}
}

func Test_ProcessImmediateHandlerCommand_PrecheckCleanupRemainsImmediate(t *testing.T) {
	setCleanupTestHandlerEnvironment(t)
	cleaned := false
	cmd := types.CmdEnableTemplate.InitializeFunctions(types.CmdFunctions{
		Pre: func(ctx *log.Context, hEnv types.HandlerEnvironment, metadata types.RCMetadata, c types.Cmd) error {
			c.Functions.Cleanup(ctx, metadata, hEnv, "")
			require.True(t, cleaned)
			return errors.New("already processed")
		},
		Cleanup: func(_ *log.Context, _ types.RCMetadata, _ types.HandlerEnvironment, _ string) {
			cleaned = true
		},
	})
	require.ErrorContains(t, ProcessImmediateHandlerCommand(cmd, cleanupTestSettings(false), "cleanup-test", 1), "already processed")
	require.True(t, cleaned)
}

func Test_ProcessHandlerCommand_CleanupOrderingUnchanged(t *testing.T) {
	hEnv := setCleanupTestHandlerEnvironment(t)
	name, seq := "cleanup-test", 1
	t.Setenv(constants.ConfigExtensionNameEnvName, name)
	t.Setenv(constants.ConfigSequenceNumberEnvName, strconv.Itoa(seq))
	ctx := log.NewContext(log.NewNopLogger())
	require.NoError(t, storeConfigSettingsFileForLocalExecution(ctx, cleanupTestSettings(false), hEnv, name, seq))
	events := []string{}
	cmd := types.CmdEnableTemplate.InitializeFunctions(types.CmdFunctions{
		Invoke: func(ctx *log.Context, env types.HandlerEnvironment, _ *types.RunCommandInstanceView, metadata types.RCMetadata, c types.Cmd) (string, string, error, int) {
			c.Functions.Cleanup(ctx, metadata, env, "")
			events = append(events, "invoke returned")
			return "", "", nil, 0
		},
		ReportStatus: func(_ *log.Context, _ types.HandlerEnvironment, _ types.RCMetadata, statusType types.StatusType, _ types.Cmd, _ string) error {
			if statusType == types.StatusSuccess {
				events = append(events, "final status")
			}
			return nil
		},
		Cleanup: func(_ *log.Context, _ types.RCMetadata, _ types.HandlerEnvironment, _ string) {
			events = append(events, "cleanup")
		},
	})
	require.NoError(t, ProcessHandlerCommand(cmd))
	require.Equal(t, []string{"cleanup", "invoke returned", "final status"}, events)
}

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

	err := executePreSteps(ctx, cmd, fakeEnv, extName, seqNum, constants.DownloadFolder)
	require.ErrorContains(t, err, "pre-check step failed")
	require.ErrorContains(t, err, "expected error")
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

	os.Setenv(constants.ConfigSequenceNumberEnvName, "-")
	_, err = getSeqNum(&ctx, fakeEnv, extName)
	require.ErrorContains(t, err, "failed to parse env variable ConfigSequenceNumber")

	os.Setenv(constants.ConfigSequenceNumberEnvName, "+")
	_, err = getSeqNum(&ctx, fakeEnv, extName)
	require.ErrorContains(t, err, "failed to parse env variable ConfigSequenceNumber")
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
	require.Nil(t, err)
	require.Equal(t, 0, actualSeqNum)
}
