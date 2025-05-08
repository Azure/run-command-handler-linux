package status

import (
	"encoding/json"
	"fmt"
	"slices"
	"sync"

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
	SequenceNumber int              `json:"sequenceNumber" validate:"required"`
	TimestampUTC   string           `json:"timestampUTC" validate:"required"`
	Status         types.StatusItem `json:"status" validate:"required"`
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

func (o *StatusObserver) OnNotify(status types.StatusEventArgs) error {
	o.ctx.Log("message", fmt.Sprintf("Processing status event for goal state with key %v", status.StatusKey))
	o.goalStateEventMap.Store(status.StatusKey, status.TopLevelStatus)
	return o.reportImmediateStatus(o.getImmediateTopLevelStatusToReport())
}

func (o *StatusObserver) getImmediateTopLevelStatusToReport() ImmediateTopLevelStatus {
	o.ctx.Log("message", "Getting all goal states from the event map with the latest status that are not empty")
	latestStatusToReport := []ImmediateStatus{}
	o.goalStateEventMap.Range(func(key, value interface{}) bool {
		// Only report the latest active status for each goal state
		if value.(types.StatusItem) != (types.StatusItem{}) {
			statusItem := value.(types.StatusItem)
			immediateStatus := ImmediateStatus{
				SequenceNumber: key.(types.GoalStateKey).SeqNumber,
				TimestampUTC:   statusItem.TimestampUTC,
				Status:         statusItem,
			}
			latestStatusToReport = append(latestStatusToReport, immediateStatus)
		}

		return true
	})

	o.ctx.Log("message", "Creating immediate status to report")
	return ImmediateTopLevelStatus{
		AggregateHandlerImmediateStatus: []ImmediateHandlerStatus{
			{
				HandlerName:              "RunCommandHandler",
				AggregateImmediateStatus: latestStatusToReport,
			},
		},
	}
}

func (o *StatusObserver) reportImmediateStatus(immediateStatus ImmediateTopLevelStatus) error {
	o.ctx.Log("message", "Marshalling immediate status into json")
	rootStatusJson, err := json.Marshal(immediateStatus)
	if err != nil {
		return fmt.Errorf("status: failed to marshal immediate status into json: %v", err)
	}

	o.ctx.Log("message", "create request to upload status to: "+o.Reporter.GetPutStatusUri())
	response, err := o.Reporter.ReportStatus(string(rootStatusJson))

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
	// TODO: Eventually we'll need to report also already processed goal states to the HGAP even if they are not in the new list.
	o.goalStateEventMap.Range(func(key, value interface{}) bool {
		if !slices.Contains(goalStateKeys, key.(types.GoalStateKey)) {
			o.ctx.Log("message", "removing goal state from the event map", "key", key)
			o.goalStateEventMap.Delete(key)
		}
		return true // continue iterating
	})
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
