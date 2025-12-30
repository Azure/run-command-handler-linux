package immediateruncommand

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/Azure/azure-extension-platform/vmextension"
	"github.com/Azure/run-command-handler-linux/internal/constants"
	"github.com/Azure/run-command-handler-linux/internal/goalstate"
	"github.com/Azure/run-command-handler-linux/internal/handlersettings"
	"github.com/Azure/run-command-handler-linux/internal/hostgacommunicator"
	"github.com/Azure/run-command-handler-linux/internal/observer"
	"github.com/Azure/run-command-handler-linux/internal/requesthelper"
	"github.com/Azure/run-command-handler-linux/internal/settings"
	"github.com/Azure/run-command-handler-linux/internal/status"
	"github.com/Azure/run-command-handler-linux/internal/types"
	"github.com/Azure/run-command-handler-linux/pkg/counterutil"
	"github.com/go-kit/kit/log"
)

const (
	maxConcurrentTasks int32 = 5
)

// ---- test seams (override in *_test.go) ----
var (
	getImmediateGoalStatesFn   = goalstate.GetImmediateRunCommandGoalStates
	handleImmediateGoalStateFn = goalstate.HandleImmediateGoalState
	reportFinalStatusFn        = goalstate.ReportFinalStatusForImmediateGoalState

	// signature validation seam (lets tests bypass crypto fields on ImmediateExtensionGoalState)
	validateSignatureFn = func(el hostgacommunicator.ImmediateExtensionGoalState) (bool, error) {
		return el.ValidateSignature()
	}

	// goroutine seam (lets tests run synchronously)
	spawnFn = func(f func()) { go f() }

	// time seam
	nowFn = func() time.Time { return time.Now().UTC() }
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
		newProcessedETag, err := processImmediateRunCommandGoalStates(ctx, communicator, lastProcessedETag)

		if err != nil {
			ctx.Log("error", handlersettings.InternalWrapErrorWithClarification(err, "could not process new immediate run command states because of an unexpected error"))
			ctx.Log("message", "sleep for 5 seconds before retrying")
			time.Sleep(time.Second * time.Duration(5))
		} else {
			if lastProcessedETag != newProcessedETag {
				ctx.Log("message", fmt.Sprintf("Resuming wait for immediate goal states. New etag: %v. Old etag: %v", newProcessedETag, lastProcessedETag))
			}

			lastProcessedETag = newProcessedETag
			time.Sleep(time.Second * time.Duration(constants.PolingIntervalInSeconds))
		}
	}
}

func processImmediateRunCommandGoalStates(ctx *log.Context, communicator hostgacommunicator.HostGACommunicator, lastProcessedETag string) (string, error) {
	executingTaskCount := executingTasks.Get()
	maxTasksToFetch := int(math.Max(float64(maxConcurrentTasks-executingTaskCount), 0))

	if executingTaskCount > 0 {
		ctx.Log("message", fmt.Sprintf("concurrent tasks: %v out of max %v", executingTaskCount, maxConcurrentTasks))
	}

	if maxTasksToFetch == 0 {
		ctx.Log("warning", "will not fetch new tasks in this iteration as we have reached maximum capacity...")
		return lastProcessedETag, nil
	}

	goalStates, newEtag, err := getImmediateGoalStatesFn(ctx, &communicator, lastProcessedETag)
	if err != nil {
		return newEtag, handlersettings.InternalWrapErrorWithClarification(err, "could not retrieve goal states for immediate run command")
	}

	// VM Settings have not changed and we should not process any new goal states
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
		return newEtag, handlersettings.InternalWrapErrorWithClarification(err, "could not get goal states to process")
	}

	if len(newGoalStates) > 0 {
		ctx.Log("message", fmt.Sprintf("trying to launch %v goal states concurrently", len(newGoalStates)))

		for idx := range newGoalStates {
			st := newGoalStates[idx]
			spawnFn(func() {
				state := st
				ctx.Log("message", "launching new goal state. Incrementing executing tasks counter")
				executingTasks.Increment()

				ctx.Log("message", "adding goal state to the event map")
				statusKey := types.GoalStateKey{ExtensionName: *state.ExtensionName, SeqNumber: *state.SeqNo, RuntimeSettingsState: *state.ExtensionState}
				defaultTopStatus := types.StatusItem{}
				status := types.StatusEventArgs{TopLevelStatus: defaultTopStatus, StatusKey: statusKey}

				notifier := &observer.Notifier{}
				notifier.Register(&goalStateEventObserver)
				notifier.Notify(status)
				startTime := nowFn().Format(time.RFC3339)
				exitCode, err := handleImmediateGoalStateFn(ctx, state, notifier)

				ctx.Log("message", "goal state has exited. Decrementing executing tasks counter")
				executingTasks.Decrement()

				// If there was an error executing the goal state, report the final status to the HGAP
				// For successful goal states, the status is reported by the usual workflow
				if err != nil {
					ctx.Log("error", "failed to execute goal state", "message", err)

					var ewc vmextension.ErrorWithClarification
					errorCode := 0
					if errors.As(err, &ewc) {
						errorCode = ewc.ErrorCode
					}

					instView := types.RunCommandInstanceView{
						ExecutionState:          types.Failed,
						ExecutionMessage:        "Execution failed",
						ExitCode:                exitCode,
						Output:                  "",
						Error:                   err.Error(),
						StartTime:               startTime,
						EndTime:                 nowFn().Format(time.RFC3339),
						ErrorClarificationValue: errorCode,
					}

					reportFinalStatusFn(ctx, notifier, statusKey, types.StatusError, &instView)
				}
			})
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
				ExitCode:         constants.ImmediateRC_CommandSkipped,
				Output:           "",
				Error:            errorMsg,
				StartTime:        time.Now().UTC().Format(time.RFC3339),
				EndTime:          time.Now().UTC().Format(time.RFC3339),
			}
			reportFinalStatusFn(ctx, notifier, statusKey, types.StatusSkipped, &instView)
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
		validSignature, err := validateSignatureFn(el)
		if err != nil {
			return nil, nil, err
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
