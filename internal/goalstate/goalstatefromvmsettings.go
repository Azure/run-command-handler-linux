package goalstate

import (
	"strings"

	"github.com/Azure/run-command-handler-linux/internal/constants"
	"github.com/Azure/run-command-handler-linux/internal/hostgacommunicator"
	"github.com/go-kit/kit/log"
	"github.com/pkg/errors"
)

func GetImmediateRunCommandGoalStates(ctx *log.Context, communicator hostgacommunicator.IHostGACommunicator, lastProcessedETag string) ([]hostgacommunicator.ExtensionGoalStates, string, error) {
	responseData, err := communicator.GetImmediateVMSettings(ctx, lastProcessedETag)
	if err != nil {
		return nil, lastProcessedETag, errors.Wrapf(err, "failed to retrieve VMSettings")
	}

	if responseData != nil && responseData.Modified && responseData.VMSettings != nil {
		ctx.Log("message", "VMSettings have been modified. New ETag: "+responseData.ETag)
		return filterImmediateRunCommandGoalStates(responseData.VMSettings.ExtensionGoalStates), responseData.ETag, nil
	}

	return []hostgacommunicator.ExtensionGoalStates{}, lastProcessedETag, nil
}

func filterImmediateRunCommandGoalStates(extensionGoalStates []hostgacommunicator.ExtensionGoalStates) []hostgacommunicator.ExtensionGoalStates {
	var result []hostgacommunicator.ExtensionGoalStates
	for _, element := range extensionGoalStates {
		if isRunCommandGoalState(element) {
			result = append(result, element)
		}
	}
	return result
}

func isRunCommandGoalState(goalState hostgacommunicator.ExtensionGoalStates) bool {
	return strings.EqualFold(goalState.Name, constants.RunCommandExtensionName)
}
