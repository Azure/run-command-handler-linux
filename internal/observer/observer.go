// Package observer provides an interface for implementing the observer pattern.
// The Observer interface defines a method for receiving notifications about status changes.
package observer

import "github.com/Azure/run-command-handler-linux/internal/types"

type Observer interface {
	// OnNotify is called when the status changes
	OnNotify(types.StatusEventArgs) error
	// OnDemandNotify is called when the observer needs to report the status immediately
	OnDemandNotify() error
}
