package status

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Azure/run-command-handler-linux/internal/constants"
	"github.com/Azure/run-command-handler-linux/internal/types"
	"github.com/go-kit/kit/log"
	"github.com/stretchr/testify/require"
)

func Test_reportStatus_fails(t *testing.T) {
	fakeEnv := types.HandlerEnvironment{}
	fakeEnv.HandlerEnvironment.StatusFolder = "/non-existing/dir/"

	metadata := types.NewRCMetadata("", 1, constants.DownloadFolder, constants.DataDir)
	err := ReportStatusToLocalFile(log.NewContext(log.NewNopLogger()), fakeEnv, metadata, types.StatusSuccess, types.CmdEnableTemplate, "")
	require.NotNil(t, err)
	require.Contains(t, err.Error(), "failed to save handler status")
}

func Test_reportStatusWithClarification_fails(t *testing.T) {
	fakeEnv := types.HandlerEnvironment{}
	fakeEnv.HandlerEnvironment.StatusFolder = "/non-existing/dir/"

	metadata := types.NewRCMetadata("", 1, constants.DownloadFolder, constants.DataDir)
	err := ReportStatusToLocalFile(log.NewContext(log.NewNopLogger()), fakeEnv, metadata, types.StatusSuccess, types.CmdEnableTemplate, "", 0)
	require.NotNil(t, err)
	require.Contains(t, err.Error(), "failed to save handler status")
}

func Test_reportStatus_fileExists(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "")
	require.Nil(t, err)
	defer os.RemoveAll(tmpDir)

	extName := "first"
	fakeEnv := types.HandlerEnvironment{}
	fakeEnv.HandlerEnvironment.StatusFolder = tmpDir

	metadata := types.NewRCMetadata(extName, 1, constants.DownloadFolder, constants.DataDir)
	require.Nil(t, ReportStatusToLocalFile(log.NewContext(log.NewNopLogger()), fakeEnv, metadata, types.StatusError, types.CmdEnableTemplate, "FOO ERROR"))

	path := filepath.Join(tmpDir, "first.1.status")
	b, err := os.ReadFile(path)
	require.Nil(t, err, ".status file exists")
	require.NotEqual(t, 0, len(b), ".status file not empty")
}

func Test_reportStatusWithClarification_fileExists(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "")
	require.Nil(t, err)
	defer os.RemoveAll(tmpDir)

	extName := "first"
	fakeEnv := types.HandlerEnvironment{}
	fakeEnv.HandlerEnvironment.StatusFolder = tmpDir

	metadata := types.NewRCMetadata(extName, 1, constants.DownloadFolder, constants.DataDir)
	require.Nil(t, ReportStatusToLocalFile(log.NewContext(log.NewNopLogger()), fakeEnv, metadata, types.StatusError, types.CmdEnableTemplate, "FOO ERROR", 0))

	path := filepath.Join(tmpDir, "first.1.status")
	b, err := os.ReadFile(path)
	require.Nil(t, err, ".status file exists")
	require.NotEqual(t, 0, len(b), ".status file not empty")
}

func Test_reportStatus_checksIfShouldBeReported(t *testing.T) {
	for _, c := range types.CmdTemplates {
		tmpDir, err := os.MkdirTemp("", "status-"+c.Name)
		require.Nil(t, err)
		defer os.RemoveAll(tmpDir)

		extName := "first"
		fakeEnv := types.HandlerEnvironment{}
		fakeEnv.HandlerEnvironment.StatusFolder = tmpDir
		metadata := types.NewRCMetadata(extName, 2, constants.DownloadFolder, constants.DataDir)
		require.Nil(t, ReportStatusToLocalFile(log.NewContext(log.NewNopLogger()), fakeEnv, metadata, types.StatusSuccess, c, ""))

		fp := filepath.Join(tmpDir, "first.2.status")
		_, err = os.Stat(fp) // check if the .status file is there
		if c.ShouldReportStatus && err != nil {
			t.Fatalf("cmd=%q should have reported status file=%q err=%v", c.Name, fp, err)
		}
		if !c.ShouldReportStatus {
			if err == nil {
				t.Fatalf("cmd=%q should not have reported status file. file=%q", c.Name, fp)
			} else if !os.IsNotExist(err) {
				t.Fatalf("cmd=%q some other error occurred. file=%q err=%q", c.Name, fp, err)
			}
		}
	}
}

func Test_reportStatusWithClarification_checksIfShouldBeReported(t *testing.T) {
	for _, c := range types.CmdTemplates {
		tmpDir, err := os.MkdirTemp("", "status-"+c.Name)
		require.Nil(t, err)
		defer os.RemoveAll(tmpDir)

		extName := "first"
		fakeEnv := types.HandlerEnvironment{}
		fakeEnv.HandlerEnvironment.StatusFolder = tmpDir
		metadata := types.NewRCMetadata(extName, 2, constants.DownloadFolder, constants.DataDir)
		require.Nil(t, ReportStatusToLocalFile(log.NewContext(log.NewNopLogger()), fakeEnv, metadata, types.StatusSuccess, c, "", 0))

		fp := filepath.Join(tmpDir, "first.2.status")
		_, err = os.Stat(fp) // check if the .status file is there
		if c.ShouldReportStatus && err != nil {
			t.Fatalf("cmd=%q should have reported status file=%q err=%v", c.Name, fp, err)
		}
		if !c.ShouldReportStatus {
			if err == nil {
				t.Fatalf("cmd=%q should not have reported status file. file=%q", c.Name, fp)
			} else if !os.IsNotExist(err) {
				t.Fatalf("cmd=%q some other error occurred. file=%q err=%q", c.Name, fp, err)
			}
		}
	}
}
func Test_getSingleStatusItem(t *testing.T) {
	ctx := log.NewContext(log.NewSyncLogger(log.NewLogfmtLogger(os.Stdout))).With("time", log.DefaultTimestamp)
	msgToReport := "Final message to report"
	statusItem, err := GetSingleStatusItem(ctx, types.StatusSuccess, types.CmdEnableTemplate, msgToReport, "test")
	require.Nil(t, err, "GetSingleStatusItem should not return an error")
	require.NotNil(t, statusItem, "GetSingleStatusItem should return a status item")
	require.Equal(t, types.StatusSuccess, statusItem.Status.Status, "GetSingleStatusItem should return a status item with the correct status")
	require.Equal(t, types.CmdEnableTemplate.Name, statusItem.Status.Operation, "GetSingleStatusItem should return a status item with the correct operation")
	require.Equal(t, msgToReport, statusItem.Status.FormattedMessage.Message, "GetSingleStatusItem should return a status item with the correct message")
}

func Test_marshalStatusReportIntoJson(t *testing.T) {
	indentEnabled := true

	for i := 0; i < 2; i++ {
		statusItem := types.StatusItem{
			Version:      2,
			TimestampUTC: "2021-09-01T12:00:00Z",
			Status: types.Status{
				Operation: "TestOperation",
				Status:    "TestStatus",
				FormattedMessage: types.FormattedMessage{
					Message: "Test message",
					Lang:    "en-US",
				},
			},
		}
		statusReport := types.StatusReport{statusItem}
		json, err := MarshalStatusReportIntoJson(statusReport, indentEnabled)
		require.Nil(t, err, "MarshalStatusReportIntoJson should not return an error")
		require.NotNil(t, json, "MarshalStatusReportIntoJson should return a json string")
		jsonStr := string(json)
		require.Contains(t, jsonStr, "TestOperation", "MarshalStatusReportIntoJson should return a json string with the correct operation")
		require.Contains(t, jsonStr, "TestStatus", "MarshalStatusReportIntoJson should return a json string with the correct status")
		require.Contains(t, jsonStr, "Test message", "MarshalStatusReportIntoJson should return a json string with the correct message")
		require.Contains(t, jsonStr, "en-US", "MarshalStatusReportIntoJson should return a json string with the correct language")
		require.Contains(t, jsonStr, "2021-09-01T12:00:00Z", "MarshalStatusReportIntoJson should return a json string with the correct timestamp")
		require.Contains(t, jsonStr, "2", "MarshalStatusReportIntoJson should return a json string with the correct version")

		// Test with indent disabled for the second iteration
		indentEnabled = false
	}
}
