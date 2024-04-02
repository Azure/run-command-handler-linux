package goalstate_test

import (
	"errors"
	"os"
	"testing"

	"github.com/Azure/run-command-handler-linux/internal/goalstate"
	"github.com/Azure/run-command-handler-linux/internal/hostgacommunicator"
	"github.com/go-kit/kit/log"
	"github.com/stretchr/testify/require"
)

type TestCommunicator struct{}

func (t *TestCommunicator) GetImmediateVMSettings(ctx *log.Context) (*hostgacommunicator.VMSettings, error) {
	return &hostgacommunicator.VMSettings{
		HostGAPluginVersion:       "1.0.8.143",
		VmSettingsSchemaVersion:   "0.0",
		ActivityId:                "cfba1e29-4d19-4f6f-a709-d2a7c413c1b9",
		CorrelationId:             "cfba1e29-4d19-4f6f-a709-d2a7c413c1b9",
		ExtensionGoalStatesSource: "FastTrack",
		ExtensionGoalStates: []hostgacommunicator.ExtensionGoalStates{
			{
				Name:    "Microsoft.CPlat.Core.RunCommandHandlerLinux",
				Version: "1.0.0",
			},
			{
				Name:    "Microsoft.CPlat.Core.RunCommandHandlerLinux",
				Version: "1.0.1",
			},
			{
				Name:    "Microsoft.CPlat.Core.RunCommandHandlerLinux",
				Version: "1.0.2",
			},
			{
				Name:    "Microsoft.CPlat.Core.NonRunCommandHandler",
				Version: "1.0.0",
			},
			{
				Name:    "Microsoft.CPlat.Core.NonRunCommandHandler",
				Version: "1.0.1",
			},
		},
	}, nil
}

type BadCommunicator struct{}

func (t *BadCommunicator) GetImmediateVMSettings(ctx *log.Context) (*hostgacommunicator.VMSettings, error) {
	return nil, errors.New("http expected failure")
}

type NilCommunicator struct{}

func (t *NilCommunicator) GetImmediateVMSettings(ctx *log.Context) (*hostgacommunicator.VMSettings, error) {
	return nil, nil
}

type EmptyCommunicator struct{}

func (t *EmptyCommunicator) GetImmediateVMSettings(ctx *log.Context) (*hostgacommunicator.VMSettings, error) {
	return &hostgacommunicator.VMSettings{}, nil
}

func Test_GetFilteredImmediateVMSettings(t *testing.T) {
	ctx := log.NewContext(log.NewSyncLogger(log.NewLogfmtLogger(os.Stdout))).With("time", log.DefaultTimestamp)
	communicator := new(TestCommunicator)
	actualIRCGoalStates, err := goalstate.GetImmediateRunCommandGoalStates(ctx, communicator)
	require.Nil(t, err)
	require.Equal(t, 3, len(actualIRCGoalStates))
}

func Test_GetFilteredImmediateVMSettingsFailedToRetrieve(t *testing.T) {
	ctx := log.NewContext(log.NewSyncLogger(log.NewLogfmtLogger(os.Stdout))).With("time", log.DefaultTimestamp)
	badCommunicator := new(BadCommunicator)
	_, err := goalstate.GetImmediateRunCommandGoalStates(ctx, badCommunicator)
	require.ErrorContains(t, err, "failed to retrieve VMSettings")
	require.ErrorContains(t, err, "http expected failure")
}

func Test_GetFilteredImmediateVMSettingsHandleEmptyResults(t *testing.T) {
	ctx := log.NewContext(log.NewSyncLogger(log.NewLogfmtLogger(os.Stdout))).With("time", log.DefaultTimestamp)
	nilCommunicator := new(NilCommunicator)
	actualIRCGoalStates, err := goalstate.GetImmediateRunCommandGoalStates(ctx, nilCommunicator)
	require.Nil(t, err)
	require.Zero(t, len(actualIRCGoalStates))

	emptyCommunicator := new(EmptyCommunicator)
	actualIRCGoalStates, err = goalstate.GetImmediateRunCommandGoalStates(ctx, emptyCommunicator)
	require.Nil(t, err)
	require.Zero(t, len(actualIRCGoalStates))
}
