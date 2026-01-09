package types

import "time"

// StatusReport contains one or more status items and is the parent object
type StatusReport []StatusItem

func NewStatusReport(statusType StatusType, operation string, message string, extName string) StatusReport {

	return []StatusItem{
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
}

func NewStatusReportWithErrorClarification(statusType StatusType, operation string, message string, extName string, errorcode int) StatusReport {
	errorClarificationName := "ErrorClarification"

	var subStatuses []subStatus

	// Add subStatus only if errorcode is non-zero
	if errorcode != 0 {
		subStatuses = append(subStatuses, subStatus{
			Name:   errorClarificationName,
			Code:   errorcode,
			Status: statusType,
		})
	}

	return []StatusItem{
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
				SubStatus: subStatuses,
			},
		},
	}
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
	Name             string           `json:"name"`
	Operation        string           `json:"operation"`
	Status           StatusType       `json:"status"`
	FormattedMessage FormattedMessage `json:"formattedMessage"`
	SubStatus        []subStatus      `json:"substatus"` // optional substatus, can be nil
}

// FormattedMessage is a struct used for serializing status
type FormattedMessage struct {
	Lang    string `json:"lang"`
	Message string `json:"message"`
}

// substatus used for serialization
// It contains neccesary info that is used in CRP for error clarification
type subStatus struct {
	// Name is the name of the substatus
	// Should be set as "ErroClarificationName"
	Name string `json:"name"`
	// Code is the code of the substatus
	// Number code that is used in CRP for error clarification in conjunction with the errorclassification file
	Code int `json:"code"`
	// Status is the status of the substatus
	// Status of the run command operation
	Status StatusType `json:"status"`
}
