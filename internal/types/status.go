package types

import (
	"time"

	platformstatus "github.com/Azure/azure-extension-platform/pkg/status"
	"github.com/Azure/run-command-handler-linux/pkg/errorutil"
)

// StatusReport contains one or more status items and is the parent object
type StatusReport []StatusItem

func NewStatusReport(statusType StatusType, operation string, message string, extName string, clarificationErrors ...error) StatusReport {
	report := []StatusItem{
		{
			Version:      1, // this is the protocol version do not change unless you are sure
			TimestampUTC: time.Now().UTC().Format(time.RFC3339),
			Status: Status{
				Name:      extName,
				Operation: operation,
				Status:    statusType,
				FormattedMessage: FormattedMessage{
					Lang:    "en",
					Message: message},
			},
		},
	}
	for _, err := range clarificationErrors {
		if code, ok := errorutil.Code(err); ok {
			report[0].Status.Substatuses = []platformstatus.Substatus{{
				Name:   platformstatus.ErrorClarificationSubStatusName,
				Status: string(StatusError),
				Code:   code,
			}}
			break
		}
	}
	return report
}

// StatusItem is used to serialize an individual part of the status read by the server
type StatusItem struct {
	Version      int    `json:"version"`
	TimestampUTC string `json:"timestampUTC"`
	Status       Status `json:"status"`
}

// StatusType reports the execution status
type StatusType string

const (
	// StatusTransitioning indicates the operation has begun but not yet completed
	StatusTransitioning StatusType = "transitioning"

	// StatusError indicates the operation failed
	StatusError StatusType = "error"

	// StatusSuccess indicates the operation succeeded
	StatusSuccess StatusType = "success"

	// StatusSkipped indicates the operation was skipped due to a precondition
	StatusSkipped StatusType = "skipped"
)

// Status is used for serializing status in a manner the server understands
type Status struct {
	Substatuses      []platformstatus.Substatus `json:"substatus,omitempty"`
	Name             string                     `json:"name"`
	Operation        string                     `json:"operation"`
	Status           StatusType                 `json:"status"`
	FormattedMessage FormattedMessage           `json:"formattedMessage"`
}

// FormattedMessage is a struct used for serializing status
type FormattedMessage struct {
	Lang    string `json:"lang"`
	Message string `json:"message"`
}
