package goalstate_test

import (
	"errors"
	"os"
	"testing"

	"github.com/Azure/azure-extension-platform/vmextension"
	"github.com/Azure/run-command-handler-linux/internal/constants"
	"github.com/Azure/run-command-handler-linux/internal/goalstate"
	"github.com/Azure/run-command-handler-linux/internal/hostgacommunicator"
	"github.com/Azure/run-command-handler-linux/internal/settings"
	"github.com/go-kit/kit/log"
	"github.com/stretchr/testify/require"
)

type TestCommunicator struct{}

func (t *TestCommunicator) GetImmediateVMSettings(ctx *log.Context, eTag string) (*hostgacommunicator.ResponseData, error) {
	extName, seqNum := "testExtension", 5
	immediateGoalState := hostgacommunicator.ImmediateExtensionGoalState{
		Name: "Microsoft.CPlat.Core.RunCommandHandlerLinux",
		Settings: []settings.SettingsCommon{
			{
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
	}

	testExtensionGoalState := hostgacommunicator.ImmediateExtensionGoalState{
		Name: "Microsoft.Azure.Extensions.Edp.RunCommandHandlerLinuxTest",
		Settings: []settings.SettingsCommon{
			{
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
	}

	nonImmediateGoalState := hostgacommunicator.ImmediateExtensionGoalState{
		Name: "Microsoft.CPlat.Core.NonRunCommandHandler",
		Settings: []settings.SettingsCommon{
			{
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
	}

	vmSettings := &hostgacommunicator.VMImmediateExtensionsGoalState{
		ImmediateExtensionGoalStates: []hostgacommunicator.ImmediateExtensionGoalState{
			immediateGoalState,
			immediateGoalState,
			immediateGoalState,
			testExtensionGoalState,
			nonImmediateGoalState,
			nonImmediateGoalState,
		},
	}
	return &hostgacommunicator.ResponseData{VMSettings: vmSettings, ETag: "123456", Modified: true}, nil
}

type BadCommunicator struct{}

func (t *BadCommunicator) GetImmediateVMSettings(ctx *log.Context, eTag string) (*hostgacommunicator.ResponseData, error) {
	return nil, errors.New("http expected failure")
}

type NilCommunicator struct{}

func (t *NilCommunicator) GetImmediateVMSettings(ctx *log.Context, eTag string) (*hostgacommunicator.ResponseData, error) {
	return nil, nil
}

type EmptyCommunicator struct{}

func (t *EmptyCommunicator) GetImmediateVMSettings(ctx *log.Context, eTag string) (*hostgacommunicator.ResponseData, error) {
	return &hostgacommunicator.ResponseData{VMSettings: &hostgacommunicator.VMImmediateExtensionsGoalState{}, ETag: "123456", Modified: true}, nil
}

func Test_GetFilteredImmediateVMSettings(t *testing.T) {
	ctx := log.NewContext(log.NewSyncLogger(log.NewLogfmtLogger(os.Stdout))).With("time", log.DefaultTimestamp)
	communicator := new(TestCommunicator)
	actualIRCGoalStates, _, err := goalstate.GetImmediateRunCommandGoalStates(ctx, communicator, "")
	require.Nil(t, err)
	require.Equal(t, 4, len(actualIRCGoalStates))
}

func Test_GetFilteredImmediateVMSettingsFailedToRetrieve(t *testing.T) {
	ctx := log.NewContext(log.NewSyncLogger(log.NewLogfmtLogger(os.Stdout))).With("time", log.DefaultTimestamp)
	badCommunicator := new(BadCommunicator)
	_, _, err := goalstate.GetImmediateRunCommandGoalStates(ctx, badCommunicator, "")
	require.ErrorContains(t, err, "http expected failure")
}

func Test_GetFilteredImmediateVMSettings_NoCommunicator(t *testing.T) {
	ctx := log.NewContext(log.NewSyncLogger(log.NewLogfmtLogger(os.Stdout))).With("time", log.DefaultTimestamp)
	_, _, err := goalstate.GetImmediateRunCommandGoalStates(ctx, nil, "")
	require.NotNil(t, err, "No error returned when one was expected")
	var ewc vmextension.ErrorWithClarification
	require.True(t, errors.As(err, &ewc), "Error is not of type ErrorWithClarification")
	require.Equal(t, constants.Hgap_InternalArgumentError, ewc.ErrorCode, "Expected error %d but received %d", constants.Hgap_InternalArgumentError, ewc.ErrorCode)
}

func Test_GetFilteredImmediateVMSettingsHandleEmptyResults(t *testing.T) {
	ctx := log.NewContext(log.NewSyncLogger(log.NewLogfmtLogger(os.Stdout))).With("time", log.DefaultTimestamp)
	nilCommunicator := new(NilCommunicator)
	actualIRCGoalStates, _, err := goalstate.GetImmediateRunCommandGoalStates(ctx, nilCommunicator, "")
	require.Nil(t, err)
	require.Zero(t, len(actualIRCGoalStates))

	emptyCommunicator := new(EmptyCommunicator)
	actualIRCGoalStates, _, err = goalstate.GetImmediateRunCommandGoalStates(ctx, emptyCommunicator, "")
	require.Nil(t, err)
	require.Zero(t, len(actualIRCGoalStates))
}
