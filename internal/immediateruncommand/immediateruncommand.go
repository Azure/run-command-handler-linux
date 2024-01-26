package immediateruncommand

import (
	"fmt"
	"math"
	"time"

	"github.com/Azure/run-command-handler-linux/internal/goalstate"
	"github.com/Azure/run-command-handler-linux/internal/hostgacommunicator"
	"github.com/Azure/run-command-handler-linux/internal/settings"
	"github.com/Azure/run-command-handler-linux/pkg/counterutil"
	"github.com/go-kit/kit/log"
	"github.com/pkg/errors"
)

const (
	maxConcurrentTasks             int32 = 5
	statePollingFrequencyInSeconds int32 = 60 // This should be almost immediate when creating a 'PENDING GET' to se the server as the HGAP server returns a response within 60 seconds
)

var executingTasks counterutil.AtomicCount

func StartImmediateRunCommand(ctx *log.Context) error {
	ctx.Log("message", "starting immediate run command service")
	communicator := hostgacommunicator.HostGACommunicator{}

	for {
		err := processImmediateRunCommandGoalStates(ctx, communicator)
		if err != nil {
			ctx.Log("error", errors.Wrapf(err, "could not process new immediate run command states"))
		}

		ctx.Log("message", "sleep for 2 minutes before the next attempt")
		time.Sleep(time.Second * time.Duration(statePollingFrequencyInSeconds))
	}
}

func processImmediateRunCommandGoalStates(ctx *log.Context, communicator hostgacommunicator.HostGACommunicator) error {
	maxTasksToFetch := int(math.Max(float64(maxConcurrentTasks-executingTasks.Get()), 0))
	ctx.Log("message", fmt.Sprintf("concurrent tasks: %v out of max %v", executingTasks.Get(), maxConcurrentTasks))
	if maxTasksToFetch == 0 {
		ctx.Log("warning", "will not fetch new tasks in this iteration as we have reached maximum capacity...")
		return nil
	}

	goalStates, err := goalstate.GetImmediateRunCommandGoalStates(ctx, &communicator)
	if err != nil {
		return errors.Wrapf(err, "could not retrieve goal states for immediate run command")
	}

	var newGoalStates []settings.SettingsCommon
	for _, el := range goalStates {
		validSignature, err := el.ValidateSignature()
		if err != nil {
			return errors.Wrap(err, "failed to validate goal state signature")
		}

		if validSignature {
			for _, s := range el.Settings {
				if len(newGoalStates) < maxTasksToFetch {
					newGoalStates = append(newGoalStates, s)
				}
			}
		}
	}

	if len(newGoalStates) > 0 {
		ctx.Log("message", fmt.Sprintf("trying to launch %v goal states concurrently", len(newGoalStates)))

		for idx := range newGoalStates {
			go func(state settings.SettingsCommon) {
				ctx.Log("message", "launching new goal state. Incrementing executing tasks counter")
				executingTasks.Increment()
				err := goalstate.HandleImmediateGoalState(ctx, state)
				ctx.Log("message", "goal state has exited. Decrementing executing tasks counter")
				executingTasks.Decrement()

				if err != nil {
					ctx.Log("error", "failed to execute goal state", "message", err)
				}
			}(newGoalStates[idx])
		}

		ctx.Log("message", "finished launching goal states")
	} else {
		ctx.Log("message", "no new goal states were found in this iteration")
	}

	return nil
}
