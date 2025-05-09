package immediateruncommand

import (
	"fmt"
	"math"
	"time"

	"github.com/Azure/run-command-handler-linux/internal/constants"
	"github.com/Azure/run-command-handler-linux/internal/goalstate"
	"github.com/Azure/run-command-handler-linux/internal/hostgacommunicator"
	"github.com/Azure/run-command-handler-linux/internal/observer"
	"github.com/Azure/run-command-handler-linux/internal/requesthelper"
	"github.com/Azure/run-command-handler-linux/internal/settings"
	"github.com/Azure/run-command-handler-linux/internal/status"
	"github.com/Azure/run-command-handler-linux/internal/types"
	"github.com/Azure/run-command-handler-linux/pkg/counterutil"
	"github.com/go-kit/kit/log"
	"github.com/pkg/errors"
)

const (
	maxConcurrentTasks int32 = 5
)

var executingTasks counterutil.AtomicCount

// goalStateEventObserver is an observer that listens for status changes in goal states.
// Each goal state is identified by a unique key and has a notifier associated with it.
// The notifiers are used to send status back to the observer and the observer reports the status to the HGAP.
var goalStateEventObserver = status.StatusObserver{}

type VMSettingsRequestManager struct{}

func (*VMSettingsRequestManager) GetVMSettingsRequestManager(ctx *log.Context) (*requesthelper.RequestManager, error) {
	return hostgacommunicator.GetVMSettingsRequestManager(ctx)
}

func StartImmediateRunCommand(ctx *log.Context) error {
	ctx.Log("message", "starting immediate run command service")
	var vmRequestManager = new(VMSettingsRequestManager)
	var lastProcessedETag string = ""
	communicator := hostgacommunicator.NewHostGACommunicator(vmRequestManager)
	goalStateEventObserver.Initialize(ctx)

	ctx.Log("message", fmt.Sprintf("Polling for goal state every %v seconds", constants.PolingIntervalInSeconds))
	for {
		ctx.Log("message", "processing new immediate run command goal states. Last processed ETag: "+lastProcessedETag)
		newProcessedETag, err := processImmediateRunCommandGoalStates(ctx, communicator, lastProcessedETag)

		if err != nil {
			ctx.Log("error", errors.Wrapf(err, "could not process new immediate run command states because of an unexpected error"))
			ctx.Log("message", "sleep for 5 seconds before retrying")
			time.Sleep(time.Second * time.Duration(5))
		} else {
			lastProcessedETag = newProcessedETag
			time.Sleep(time.Second * time.Duration(constants.PolingIntervalInSeconds))
		}
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

	// VMSettings has not changed and we should not process any new goal states
	if newEtag == lastProcessedETag {
		return newEtag, nil
	}

	var goalStateKeys []types.GoalStateKey
	for _, s := range goalStates {
		for _, setting := range s.Settings {
			if setting.ExtensionState != nil {
				goalStateKeys = append(goalStateKeys, types.GoalStateKey{ExtensionName: *setting.ExtensionName, SeqNumber: *setting.SeqNo, RuntimeSettingsState: *setting.ExtensionState})
			}
		}
	}
	goalStateEventObserver.RemoveProcessedGoalStates(goalStateKeys)
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
				statusKey := types.GoalStateKey{ExtensionName: *state.ExtensionName, SeqNumber: *state.SeqNo, RuntimeSettingsState: *state.ExtensionState}
				defaultTopStatus := types.StatusItem{}
				status := types.StatusEventArgs{TopLevelStatus: defaultTopStatus, StatusKey: statusKey}

				notifier := &observer.Notifier{}
				notifier.Register(&goalStateEventObserver)
				notifier.Notify(status)
				startTime := time.Now().UTC().Format(time.RFC3339)
				exitCode, err := goalstate.HandleImmediateGoalState(ctx, state, notifier)

				ctx.Log("message", "goal state has exited. Decrementing executing tasks counter")
				executingTasks.Decrement()

				// If there was an error executing the goal state, report the final status to the HGAP
				// For successful goal states, the status is reported by the usual workflow
				if err != nil {
					ctx.Log("error", "failed to execute goal state", "message", err)
					instView := types.RunCommandInstanceView{
						ExecutionState:   types.Failed,
						ExecutionMessage: "Execution failed",
						ExitCode:         exitCode,
						Output:           "",
						Error:            err.Error(),
						StartTime:        startTime,
						EndTime:          time.Now().UTC().Format(time.RFC3339),
					}
					goalstate.ReportFinalStatusForImmediateGoalState(ctx, notifier, statusKey, types.StatusError, &instView)

				}
			}(newGoalStates[idx])
		}

		ctx.Log("message", "finished launching goal states")
	} else {
		ctx.Log("message", "no new goal states were found in this iteration")
	}

	if len(skippedGoalStates) > 0 {
		ctx.Log("message", fmt.Sprintf("skipped %v goal states due to reaching the maximum concurrent tasks", len(skippedGoalStates)))
		for _, skippedGoalState := range skippedGoalStates {
			statusKey := types.GoalStateKey{ExtensionName: *skippedGoalState.ExtensionName, SeqNumber: *skippedGoalState.SeqNo, RuntimeSettingsState: *skippedGoalState.ExtensionState}
			notifier := &observer.Notifier{}
			notifier.Register(&goalStateEventObserver)

			errorMsg := fmt.Sprintf("Exceeded concurrent goal state processing limit. Allowed new goal state count: %d. Extension: %s, SeqNumber: %d", maxTasksToFetch, *skippedGoalState.ExtensionName, *skippedGoalState.SeqNo)
			instView := types.RunCommandInstanceView{
				ExecutionState:   types.Failed,
				ExecutionMessage: "Execution was skipped due to reaching the maximum concurrent tasks",
				ExitCode:         constants.ExitCode_SkippedImmediateGoalState,
				Output:           "",
				Error:            errorMsg,
				StartTime:        time.Now().UTC().Format(time.RFC3339),
				EndTime:          time.Now().UTC().Format(time.RFC3339),
			}
			goalstate.ReportFinalStatusForImmediateGoalState(ctx, notifier, statusKey, types.StatusSkipped, &instView)
		}
	} else {
		ctx.Log("message", "no goal states were skipped")
	}

	return newEtag, nil
}

// Get the goal states that have not been processed yet
func getGoalStatesToProcess(goalStates []hostgacommunicator.ImmediateExtensionGoalState, maxTasksToFetch int) ([]settings.SettingsCommon, []settings.SettingsCommon, error) {
	var newGoalStates []settings.SettingsCommon
	var skippedGoalStates []settings.SettingsCommon
	for _, el := range goalStates {
		validSignature, err := el.ValidateSignature()
		if err != nil {
			return nil, nil, errors.Wrap(err, "failed to validate goal state signature")
		}

		if validSignature {
			for _, s := range el.Settings {
				statusKey := types.GoalStateKey{ExtensionName: *s.ExtensionName, SeqNumber: *s.SeqNo, RuntimeSettingsState: *s.ExtensionState}
				_, goalStateAlreadyProcessed := goalStateEventObserver.GetStatusForKey(statusKey)
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
