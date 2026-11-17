package immediateruncommand

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Azure/run-command-handler-linux/internal/constants"
	"github.com/Azure/run-command-handler-linux/internal/handlersettings"
	"github.com/Azure/run-command-handler-linux/internal/hostgacommunicator"
	"github.com/Azure/run-command-handler-linux/internal/settings"
	"github.com/Azure/run-command-handler-linux/internal/types"
	"github.com/Azure/run-command-handler-linux/pkg/statusreporter"
	"github.com/go-kit/kit/log"
	"github.com/stretchr/testify/require"
)

func Test_getGoalStatesToProcess_preservesHandlerIdentity(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, handlersettings.HandlerEnvFileName), []byte(`[{}]`), 0600))
	require.NoError(t, os.Mkdir(filepath.Join(root, "status"), 0755))
	oldExecutable := os.Args[0]
	os.Args[0] = filepath.Join(root, "handler")
	t.Cleanup(func() { os.Args[0] = oldExecutable })
	t.Setenv(constants.ExtensionPathEnvName, root)
	goalStateEventObserver.Initialize(log.NewContext(log.NewNopLogger()))
	goalStateEventObserver.Reporter = statusreporter.TestGuestInformationClient{Endpoint: "http://localhost/upload"}

	name, state, seq := "same-command", "enabled", 1
	setting := settings.SettingsCommon{ExtensionName: &name, ExtensionState: &state, SeqNo: &seq}
	goals := []hostgacommunicator.ImmediateExtensionGoalState{
		{Name: constants.RunCommandExtensionName, Settings: []settings.SettingsCommon{setting}},
		{Name: constants.RunCommandTestExtensionName, Settings: []settings.SettingsCommon{setting}},
	}
	ready, skipped, err := getGoalStatesToProcess(goals, 1)
	require.NoError(t, err)
	require.Len(t, ready, 1)
	require.Len(t, skipped, 1)
	require.Equal(t, constants.RunCommandExtensionName, ready[0].handlerName)
	require.Equal(t, constants.RunCommandTestExtensionName, skipped[0].handlerName)
	require.Equal(t, setting, ready[0].SettingsCommon)
	require.Equal(t, setting, skipped[0].SettingsCommon)

	require.NoError(t, goalStateEventObserver.OnNotify(types.StatusEventArgs{
		StatusKey: types.GoalStateKey{
			HandlerName: constants.RunCommandExtensionName, ExtensionName: name, SeqNumber: seq, RuntimeSettingsState: state,
		},
	}))
	ready, skipped, err = getGoalStatesToProcess(goals, 2)
	require.NoError(t, err)
	require.Len(t, ready, 1, "processing production must not suppress the test handler's command with the same name and sequence")
	require.Equal(t, constants.RunCommandTestExtensionName, ready[0].handlerName)
	require.Empty(t, skipped)
}
