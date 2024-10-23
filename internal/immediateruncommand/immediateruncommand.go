package immediateruncommand

import (
	"fmt"
	"math"
	"slices"
	"sync"
	"time"

	"github.com/Azure/run-command-handler-linux/internal/goalstate"
	"github.com/Azure/run-command-handler-linux/internal/hostgacommunicator"
	"github.com/Azure/run-command-handler-linux/internal/requesthelper"
	"github.com/Azure/run-command-handler-linux/internal/settings"
	"github.com/Azure/run-command-handler-linux/internal/types"
	"github.com/Azure/run-command-handler-linux/pkg/counterutil"
	"github.com/go-kit/kit/log"
	"github.com/pkg/errors"
)

const (
	maxConcurrentTasks int32 = 5
)

var executingTasks counterutil.AtomicCount

type goalStateKey struct {
	ExtensionName string
	SeqNumber     int
}

// goalStateEventMap is a map that stores the goal state key and the status item
// sync.Map is preferred over map because it is safe for concurrent use
var goalStateEventMap sync.Map

type VMSettingsRequestManager struct{}

func (*VMSettingsRequestManager) GetVMSettingsRequestManager(ctx *log.Context) (*requesthelper.RequestManager, error) {
	return hostgacommunicator.GetVMSettingsRequestManager(ctx)
}

func StartImmediateRunCommand(ctx *log.Context) error {
	ctx.Log("message", "starting immediate run command service")
	var vmRequestManager = new(VMSettingsRequestManager)
	var lastProcessedETag string = ""
	communicator := hostgacommunicator.NewHostGACommunicator(vmRequestManager)

	for {
		ctx.Log("message", "processing new immediate run command goal states. Last processed ETag: "+lastProcessedETag)
		newProcessedETag, err := processImmediateRunCommandGoalStates(ctx, communicator, lastProcessedETag)

		if err != nil {
			ctx.Log("error", errors.Wrapf(err, "could not process new immediate run command states"))
			ctx.Log("message", "sleep for 5 seconds before retrying")
			time.Sleep(time.Second * time.Duration(5))
		} else {
			lastProcessedETag = newProcessedETag
		}

		// Sleep for 1 second before the next iteration
		time.Sleep(time.Second)
	}
}

func processImmediateRunCommandGoalStates(ctx *log.Context, communicator hostgacommunicator.HostGACommunicator, lastProcessedETag string) (string, error) {
	maxTasksToFetch := int(math.Max(float64(maxConcurrentTasks-executingTasks.Get()), 0))
	ctx.Log("message", fmt.Sprintf("concurrent tasks: %v out of max %v", executingTasks.Get(), maxConcurrentTasks))
	if maxTasksToFetch == 0 {
		ctx.Log("warning", "will not fetch new tasks in this iteration as we have reached maximum capacity...")
		return lastProcessedETag, nil
	}

	goalStates, newEtag, err := goalstate.GetImmediateRunCommandGoalStates(ctx, &communicator, lastProcessedETag)
	if err != nil {
		return newEtag, errors.Wrapf(err, "could not retrieve goal states for immediate run command")
	}

	removeProcessedGoalStates(ctx, goalStates)
	newGoalStates, skippedGoalStates, err := getGoalStatesToProcess(goalStates, maxTasksToFetch)
	if err != nil {
		return newEtag, errors.Wrap(err, "could not get goal states to process")
	}

	if len(newGoalStates) > 0 {
		ctx.Log("message", fmt.Sprintf("trying to launch %v goal states concurrently", len(newGoalStates)))

		for idx := range newGoalStates {
			go func(state settings.SettingsCommon) {
				ctx.Log("message", "launching new goal state. Incrementing executing tasks counter")
				executingTasks.Increment()

				ctx.Log("message", "adding goal state to the event map")
				key := goalStateKey{*state.ExtensionName, *state.SeqNo}
				goalStateEventMap.Store(key, types.StatusItem{})

				err := goalstate.HandleImmediateGoalState(ctx, state)

				ctx.Log("message", "removing goal state from the event map")
				goalStateEventMap.Delete(key)

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

	if len(skippedGoalStates) > 0 {
		ctx.Log("message", fmt.Sprintf("skipped %v goal states due to reaching the maximum concurrent tasks", len(skippedGoalStates)))
		// TODO: Report skipped goal states status
	} else {
		ctx.Log("message", "no goal states were skipped")
	}

	return newEtag, nil
}

// Remove the goal states that have already been processed from the event map
func removeProcessedGoalStates(ctx *log.Context, goalStates []hostgacommunicator.ExtensionGoalStates) {
	var goalStateKeys []goalStateKey
	for _, s := range goalStates {
		for _, setting := range s.Settings {
			goalStateKeys = append(goalStateKeys, goalStateKey{*setting.ExtensionName, *setting.SeqNo})
		}
	}

	goalStateEventMap.Range(func(key, value interface{}) bool {
		if !slices.Contains(goalStateKeys, key.(goalStateKey)) {
			ctx.Log("message", "removing goal state from the event map", "key", key)
			goalStateEventMap.Delete(key)
		}
		return true // continue iterating
	})
}

// Get the goal states that have not been processed yet
func getGoalStatesToProcess(goalStates []hostgacommunicator.ExtensionGoalStates, maxTasksToFetch int) ([]settings.SettingsCommon, []settings.SettingsCommon, error) {
	var newGoalStates []settings.SettingsCommon
	var skippedGoalStates []settings.SettingsCommon
	for _, el := range goalStates {
		validSignature, err := el.ValidateSignature()
		if err != nil {
			return nil, nil, errors.Wrap(err, "failed to validate goal state signature")
		}

		if validSignature {
			for _, s := range el.Settings {
				_, goalStateAlreadyProcessed := goalStateEventMap.Load(goalStateKey{*s.ExtensionName, *s.SeqNo})
				if !goalStateAlreadyProcessed {
					if len(newGoalStates) < maxTasksToFetch {
						newGoalStates = append(newGoalStates, s)
					} else {
						skippedGoalStates = append(skippedGoalStates, s)
					}
				}
			}
		}
	}

	return newGoalStates, skippedGoalStates, nil
}
