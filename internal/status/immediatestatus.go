package status

import (
	"encoding/json"
	"fmt"
	"slices"
	"sync"

	"github.com/Azure/run-command-handler-linux/internal/handlersettings"
	"github.com/Azure/run-command-handler-linux/internal/hostgacommunicator"
	"github.com/Azure/run-command-handler-linux/internal/types"
	"github.com/Azure/run-command-handler-linux/pkg/statusreporter"
	"github.com/go-kit/kit/log"
	"github.com/pkg/errors"
)

// This is the serializable data contract for VM Aggregate Immediate Status in CRP
type ImmediateTopLevelStatus struct {
	AggregateHandlerImmediateStatus []ImmediateHandlerStatus `json:"aggregateHandlerImmediateStatus" validate:"required"`
}

// Status of the handler that is capable of handling immediate goal states
type ImmediateHandlerStatus struct {
	HandlerName              string            `json:"handlerName" validate:"required"`
	AggregateImmediateStatus []ImmediateStatus `json:"aggregateImmediateStatus" validate:"required"`
}

// Status of an immediate extension processed by a given handler
type ImmediateStatus struct {
	HandlerName    string       `json:"-"`
	SequenceNumber int          `json:"sequenceNumber" validate:"required"`
	TimestampUTC   string       `json:"timestampUTC" validate:"required"`
	Status         types.Status `json:"status" validate:"required"`
}

// Observer defines a type that can receive notifications from a Notifier.
// Must implement the Observer interface.
type StatusObserver struct {
	// goalStateEventMap is a map that stores the goal state key and the status item
	// sync.Map is preferred over map because it is safe for concurrent use
	goalStateEventMap sync.Map

	// ctx is the logger context
	ctx *log.Context

	// Reporter is the status Reporter
	Reporter statusreporter.IGuestInformationServiceClient
}

func (o *StatusObserver) Initialize(ctx *log.Context) {
	o.goalStateEventMap = sync.Map{}
	o.ctx = ctx
	o.Reporter = statusreporter.NewGuestInformationServiceClient(hostgacommunicator.WireServerFallbackAddress)
}

func (o *StatusObserver) OnDemandNotify() error {
	return o.reportImmediateStatus(o.getImmediateTopLevelStatusToReport())
}

func (o *StatusObserver) OnNotify(status types.StatusEventArgs) error {
	if status.StatusKey.HandlerName == "" {
		return errors.New("cannot report immediate status without a handler name")
	}
	o.ctx.Log("message", fmt.Sprintf("Processing status event for goal state with key %v", status.StatusKey))
	o.goalStateEventMap.Store(status.StatusKey, status.TopLevelStatus)
	// The local copy is for debugging only; HTTP reporting remains authoritative.
	if status.TopLevelStatus != (types.StatusItem{}) {
		if err := saveImmediateStatusToLocalFile(status); err != nil {
			o.ctx.Log("message", "failed to save local immediate status debug copy; continuing HTTP reporting",
				"goalState", status.StatusKey, "error", err)
		}
	}
	return o.OnDemandNotify()
}

func saveImmediateStatusToLocalFile(status types.StatusEventArgs) error {
	hEnv, err := handlersettings.GetHandlerEnv()
	if err != nil {
		return errors.Wrap(err, "could not get handler environment for immediate status debug copy")
	}
	if hEnv.HandlerEnvironment.StatusFolder == "" {
		return errors.New("handler environment has no status folder for immediate status debug copy")
	}
	data, err := MarshalStatusReportIntoJson(types.StatusReport{status.TopLevelStatus}, true)
	if err != nil {
		return errors.Wrap(err, "could not marshal immediate status debug copy")
	}
	return SaveStatusReport(hEnv.HandlerEnvironment.StatusFolder, status.StatusKey.ExtensionName, status.StatusKey.SeqNumber, data)
}

func (o *StatusObserver) getImmediateTopLevelStatusToReport() ImmediateTopLevelStatus {
	latestStatusToReport := []ImmediateStatus{}
	statusByHandler := map[string][]ImmediateStatus{}
	goalStateKeysToCheckToRemove := []types.GoalStateKey{}
	newStatusInTerminalState := []ImmediateStatus{}

	o.ctx.Log("message", "Getting all goal states from the event map with the latest status that are not empty or disabled")
	o.goalStateEventMap.Range(func(key, value interface{}) bool {
		// Only report the latest active status for each goal state
		goalStateKey := key.(types.GoalStateKey)
		statusByHandler[goalStateKey.HandlerName] = []ImmediateStatus{}
		if goalStateKey.RuntimeSettingsState != "disabled" {
			if value.(types.StatusItem) != (types.StatusItem{}) {
				o.ctx.Log("message", fmt.Sprintf("Goal state %v is not empty. Processing it.", goalStateKey))
				statusItem := value.(types.StatusItem)
				immediateStatus := ImmediateStatus{
					HandlerName:    goalStateKey.HandlerName,
					SequenceNumber: goalStateKey.SeqNumber,
					TimestampUTC:   statusItem.TimestampUTC,
					Status:         statusItem.Status,
				}

				if types.IsImmediateGoalStateInTerminalState(statusItem.Status) {
					o.ctx.Log("message", fmt.Sprintf("Goal state %v is in terminal state. Adding it to the new terminal state list.", goalStateKey))
					newStatusInTerminalState = append(newStatusInTerminalState, immediateStatus)

					// Remove the goal state from the event map since it is in terminal state. The state will be saved in the local status file.
					o.goalStateEventMap.Delete(key)
				} else {
					o.ctx.Log("message", fmt.Sprintf("Goal state %v is not in terminal state. Adding it to the latest status to report.", goalStateKey))
					latestStatusToReport = append(latestStatusToReport, immediateStatus)
				}
				goalStateKeysToCheckToRemove = append(goalStateKeysToCheckToRemove, goalStateKey)
			} else {
				o.ctx.Log("message", fmt.Sprintf("Goal state %v is empty. Not reporting status.", goalStateKey))
			}
		} else {
			o.ctx.Log("message", fmt.Sprintf("Goal state %v is disabled. Not reporting status.", goalStateKey))
			goalStateKeysToCheckToRemove = append(goalStateKeysToCheckToRemove, goalStateKey)
		}

		return true
	})

	err := RemoveDisabledAndUpdatedGoalStatesInLocalStatusFile(o.ctx, goalStateKeysToCheckToRemove)
	if err != nil {
		o.ctx.Log("error", "failed to remove disabled goal states from the local status file", "message", err)
	}

	statusInTerminalState, err := GetGoalStatesInTerminalStatus(o.ctx)
	if err != nil {
		o.ctx.Log("error", "failed to get goal states in terminal status from file. Proceeding to report the latest status from the event map", "message", err)
	}

	if len(newStatusInTerminalState) > 0 {
		o.ctx.Log("message", fmt.Sprintf("Merging %v goal states in terminal state with local file status", len(newStatusInTerminalState)))
		statusInTerminalState = append(statusInTerminalState, newStatusInTerminalState...)

		err = SaveGoalStatesInTerminalStatus(o.ctx, statusInTerminalState)
		if err != nil {
			o.ctx.Log("error", "failed to save goal states in terminal status", "message", err)
		}
	}

	if len(statusInTerminalState) > 0 {
		o.ctx.Log("message", fmt.Sprintf("Merging %v goal states in terminal state with the latest status to report", len(statusInTerminalState)))
		latestStatusToReport = append(latestStatusToReport, statusInTerminalState...)
	}

	o.ctx.Log("message", "Creating immediate status to report")
	for _, status := range latestStatusToReport {
		statusByHandler[status.HandlerName] = append(statusByHandler[status.HandlerName], status)
	}
	handlerNames := make([]string, 0, len(statusByHandler))
	for name := range statusByHandler {
		handlerNames = append(handlerNames, name)
	}
	slices.Sort(handlerNames)
	handlers := make([]ImmediateHandlerStatus, 0, len(handlerNames))
	for _, name := range handlerNames {
		handlers = append(handlers, ImmediateHandlerStatus{
			HandlerName:              name,
			AggregateImmediateStatus: statusByHandler[name],
		})
	}
	return ImmediateTopLevelStatus{
		AggregateHandlerImmediateStatus: handlers,
	}
}

func (o *StatusObserver) reportImmediateStatus(immediateStatus ImmediateTopLevelStatus) error {
	o.ctx.Log("message", "Marshalling immediate status into json")
	rootStatusJson, err := json.Marshal(immediateStatus)
	if err != nil {
		return fmt.Errorf("status: failed to marshal immediate status into json: %v", err)
	}

	o.ctx.Log("message", "create request to upload status to: "+o.Reporter.GetPutStatusUri())
	response, err := o.Reporter.ReportStatus(o.ctx, string(rootStatusJson))

	o.ctx.Log("message", fmt.Sprintf("Status received from request to %v: %v", response.Request.URL, response.Status))
	if err != nil {
		return errors.Wrap(err, "failed to report status to HGAP")
	}

	if response.StatusCode != 200 {
		return errors.New("failed to report status with error code " + response.Status)
	}

	return nil
}

// Remove the goal states that have already been processed from the event map or are disabled
// If the goal state that was added before is not in the new list of goal states, it should be removed
// This is to ensure that the event map only contains the goal states that are currently being processed
func (o *StatusObserver) RemoveProcessedGoalStates(goalStateKeys []types.GoalStateKey) {
	newStatusInTerminalState := []ImmediateStatus{}
	goalStateKeysToRemove := []types.GoalStateKey{}
	o.goalStateEventMap.Range(func(key, value interface{}) bool {
		if !slices.Contains(goalStateKeys, key.(types.GoalStateKey)) {
			goalStateKey := key.(types.GoalStateKey)
			o.ctx.Log("message", fmt.Sprintf("Goal state %v is not in the new list of goal states. Removing it from the event map.", goalStateKey))

			goalStateKeysToRemove = append(goalStateKeysToRemove, goalStateKey)
			if goalStateKey.RuntimeSettingsState != "disabled" {
				statusItem := value.(types.StatusItem)
				immediateStatus := ImmediateStatus{
					HandlerName:    goalStateKey.HandlerName,
					SequenceNumber: goalStateKey.SeqNumber,
					TimestampUTC:   statusItem.TimestampUTC,
					Status:         statusItem.Status,
				}
				newStatusInTerminalState = append(newStatusInTerminalState, immediateStatus)
				o.goalStateEventMap.Delete(key)
			} else {
				o.ctx.Log("message", fmt.Sprintf("Goal state %v is disabled. Not reporting status to indicate HGAP that it is disabled.", goalStateKey))
				o.goalStateEventMap.Delete(key)
			}
		}
		return true // continue iterating
	})

	// This should not occur since the goal state keys are already removed from the event map but adding this for safety
	// in case the previous attempt to remove the goal state keys from the local status file failed
	err := RemoveDisabledAndUpdatedGoalStatesInLocalStatusFile(o.ctx, goalStateKeysToRemove)
	if err != nil {
		o.ctx.Log("error", "failed to remove disabled goal states from the local status file", "message", err)
	}

	statusInTerminalState, err := GetGoalStatesInTerminalStatus(o.ctx)
	if err != nil {
		o.ctx.Log("error", "failed to get goal states in terminal status from file.", "message", err)
	}

	if len(newStatusInTerminalState) > 0 {
		o.ctx.Log("message", fmt.Sprintf("Merging %v goal states in terminal state with the new status to report", len(newStatusInTerminalState)))
		statusInTerminalState = append(statusInTerminalState, newStatusInTerminalState...)
	}
	err = SaveGoalStatesInTerminalStatus(o.ctx, statusInTerminalState)
	if err != nil {
		o.ctx.Log("error", "failed to save goal states in terminal status", "message", err)
	}

	o.ctx.Log("message", "Notifying the observer to report the status to HGAP")
	o.OnDemandNotify()
}

func (o *StatusObserver) GetStatusForKey(key types.GoalStateKey) (types.StatusItem, bool) {
	data, ok := o.goalStateEventMap.Load(key)
	if ok {
		return data.(types.StatusItem), true
	}

	return types.StatusItem{}, false
}

func (o *StatusObserver) getStatusForAllKeys() []types.StatusItem {
	statusItems := []types.StatusItem{}
	o.goalStateEventMap.Range(func(key, value interface{}) bool {
		statusItems = append(statusItems, value.(types.StatusItem))
		return true
	})
	return statusItems
}
