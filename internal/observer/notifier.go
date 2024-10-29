// Notifier is an interface that defines methods for managing observers and notifying them of status changes.
// It allows observers to register, unregister, and receive notifications.
package observer

import (
	"sync"

	"github.com/Azure/run-command-handler-linux/internal/types"
)

type Notifier struct {
	observer Observer
	mu       sync.Mutex
}

func (n *Notifier) Register(o Observer) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.observer = o
}

func (n *Notifier) Unregister() {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.observer = nil
}

func (n *Notifier) Notify(status types.StatusEventArgs) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.observer != nil {
		return n.observer.OnNotify(status)
	}

	return nil
}
