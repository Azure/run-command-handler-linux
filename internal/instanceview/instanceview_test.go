package instanceview

import (
	"encoding/json"
	"io/ioutil"
	"path/filepath"
	"testing"
	"time"

	"github.com/Azure/run-command-handler-linux/internal/cleanup"
	"github.com/Azure/run-command-handler-linux/internal/status"
	"github.com/Azure/run-command-handler-linux/internal/types"
	"github.com/go-kit/kit/log"
	"github.com/stretchr/testify/require"
)

func Test_serializeInstanceView(t *testing.T) {
	instanceView := types.RunCommandInstanceView{
		ExecutionState:   types.Running,
		ExecutionMessage: "Completed",
		Output:           "Script output stream with \\ \n \t \"  ",
		Error:            "Script error stream",
		StartTime:        time.Date(2000, 2, 1, 12, 30, 0, 0, time.UTC).Format(time.RFC3339),
		EndTime:          time.Date(2000, 2, 1, 12, 35, 0, 0, time.UTC).Format(time.RFC3339),
	}
	msg, err := serializeInstanceView(&instanceView)
	require.Nil(t, err)
	require.NotNil(t, msg)
	expectedMsg := "{\"executionState\":\"Running\",\"executionMessage\":\"Completed\",\"output\":\"Script output stream with \\\\ \\n \\t \\\"  \",\"error\":\"Script error stream\",\"exitCode\":0,\"startTime\":\"2000-02-01T12:30:00Z\",\"endTime\":\"2000-02-01T12:35:00Z\"}"
	require.Equal(t, expectedMsg, msg)

	var iv types.RunCommandInstanceView
	json.Unmarshal([]byte(msg), &iv)
	require.Equal(t, instanceView, iv)
}

func Test_reportInstanceView(t *testing.T) {
	instanceView := types.RunCommandInstanceView{
		ExecutionState:   types.Running,
		ExecutionMessage: "Completed",
		Output:           "Script output stream with \\ \n \t \"  ",
		Error:            "Script error stream",
		StartTime:        time.Date(2000, 2, 1, 12, 30, 0, 0, time.UTC).Format(time.RFC3339),
		EndTime:          time.Date(2000, 2, 1, 12, 35, 0, 0, time.UTC).Format(time.RFC3339),
	}
	tmpDir, err := ioutil.TempDir("", "")
	require.Nil(t, err)
	//defer os.RemoveAll(tmpDir)

	extName := "first"
	fakeEnv := types.HandlerEnvironment{}
	fakeEnv.HandlerEnvironment.StatusFolder = tmpDir

	metadata := types.NewRCMetadata(extName, 1)
	cmd := types.CmdEnableTemplate.InitializeFunctions(types.CmdFunctions{Invoke: nil, Pre: nil, ReportStatus: status.ReportStatusToLocalFile, Cleanup: cleanup.RunCommandCleanup})
	require.Nil(t, ReportInstanceView(log.NewContext(log.NewNopLogger()), fakeEnv, metadata, types.StatusSuccess, cmd, &instanceView))

	path := filepath.Join(tmpDir, extName+"."+"1.status")
	b, err := ioutil.ReadFile(path)
	require.Nil(t, err, ".status file exists")
	require.NotEqual(t, 0, len(b), ".status file not empty")

	var r types.StatusReport
	json.Unmarshal(b, &r)
	require.Equal(t, 1, len(r))
	require.Equal(t, types.StatusSuccess, r[0].Status.Status)
	require.Equal(t, types.CmdEnableTemplate.Name, r[0].Status.Operation)

	msg, _ := serializeInstanceView(&instanceView)
	require.Equal(t, msg, r[0].Status.FormattedMessage.Message)
}
