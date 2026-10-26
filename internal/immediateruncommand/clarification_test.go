package immediateruncommand

import (
	"testing"

	"github.com/Azure/run-command-handler-linux/internal/hostgacommunicator"
	"github.com/Azure/run-command-handler-linux/internal/settings"
	"github.com/Azure/run-command-handler-linux/pkg/errorutil"
	"github.com/stretchr/testify/require"
)

func TestImmediateRunCommandClarifications(t *testing.T) {
	name, state, seq := "test", "enabled", 1
	valid := settings.SettingsCommon{ExtensionName: &name, ExtensionState: &state, SeqNo: &seq}
	require.NoError(t, validateGoalStateSettings(valid))
	for _, s := range []settings.SettingsCommon{
		{},
		{ExtensionName: &name, SeqNo: &seq},
		{ExtensionName: &name, ExtensionState: &state},
		{ExtensionState: &state, SeqNo: &seq},
	} {
		_, _, err := getGoalStatesToProcess([]hostgacommunicator.ImmediateExtensionGoalState{{Settings: []settings.SettingsCommon{s}}}, 1)
		code, ok := errorutil.Code(err)
		require.True(t, ok)
		require.Equal(t, errorutil.Internal_incompleteFetchedGoalState, code)
	}
	err := newConcurrencyLimitError(5, valid)
	code, ok := errorutil.Code(err)
	require.True(t, ok)
	require.Equal(t, errorutil.CommandExecution_concurrentGoalStateLimitExceeded, code)
	require.Contains(t, err.Error(), "Allowed new goal state count: 5")
}
