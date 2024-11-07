// Notifier is an interface that defines methods for managing observers and notifying them of status changes.
// It allows observers to register, unregister, and receive notifications.
package observer

import (
	"github.com/Azure/run-command-handler-linux/internal/types"
)

type Notifier struct {
	observer Observer
}

func (n *Notifier) Register(o Observer) {
	n.observer = o
}

func (n *Notifier) Unregister() {
	n.observer = nil
}

func (n *Notifier) Notify(status types.StatusEventArgs) error {
	tempObserver := n.observer
	if tempObserver != nil {
		return tempObserver.OnNotify(status)
	}

	return nil
}
