package goalstate

import (
	"strings"

	"github.com/Azure/run-command-handler-linux/internal/constants"
	"github.com/Azure/run-command-handler-linux/internal/hostgacommunicator"
	"github.com/go-kit/kit/log"
	"github.com/pkg/errors"
)

func GetImmediateRunCommandGoalStates(ctx *log.Context, communicator hostgacommunicator.IHostGACommunicator, lastProcessedETag string) ([]hostgacommunicator.ImmediateExtensionGoalState, string, error) {
	responseData, err := communicator.GetImmediateVMSettings(ctx, lastProcessedETag)
	if err != nil {
		return nil, lastProcessedETag, errors.Wrapf(err, "failed to retrieve VMSettings")
	}

	if responseData != nil && responseData.Modified {
		ctx.Log("message", "a new response was received with ETag: "+responseData.ETag)
		if responseData.VMSettings != nil {
			return filterImmediateRunCommandGoalStates(responseData.VMSettings.ImmediateExtensionGoalStates), responseData.ETag, nil
		} else {
			return []hostgacommunicator.ImmediateExtensionGoalState{}, responseData.ETag, nil
		}
	}

	return []hostgacommunicator.ImmediateExtensionGoalState{}, lastProcessedETag, nil
}

func filterImmediateRunCommandGoalStates(extensionGoalStates []hostgacommunicator.ImmediateExtensionGoalState) []hostgacommunicator.ImmediateExtensionGoalState {
	var result []hostgacommunicator.ImmediateExtensionGoalState
	for _, element := range extensionGoalStates {
		if isRunCommandGoalState(element) {
			result = append(result, element)
		}
	}
	return result
}

func isRunCommandGoalState(goalState hostgacommunicator.ImmediateExtensionGoalState) bool {
	return strings.EqualFold(goalState.Name, constants.RunCommandExtensionName)
}
