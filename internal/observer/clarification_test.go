package observer

import (
	"errors"
	"testing"

	"github.com/Azure/run-command-handler-linux/internal/types"
	"github.com/Azure/run-command-handler-linux/pkg/errorutil"
	"github.com/stretchr/testify/require"
)

type failingObserver struct{ err error }

func (o failingObserver) OnNotify(types.StatusEventArgs) error { return o.err }
func (o failingObserver) OnDemandNotify() error                { return o.err }

func TestObserverClarification(t *testing.T) {
	cause := errors.New("observer failed")
	n := Notifier{}
	n.Register(failingObserver{cause})
	err := n.Notify(types.StatusEventArgs{})
	code, ok := errorutil.Code(err)
	require.True(t, ok)
	require.Equal(t, errorutil.Internal_failedToNotifyObserver, code)
	require.ErrorIs(t, err, cause)
	lower := errorutil.WithCode(errorutil.HostGA_failedToReportImmediateStatus, cause)
	n.Register(failingObserver{lower})
	err = n.Notify(types.StatusEventArgs{})
	require.Same(t, lower, err)
	code, ok = errorutil.Code(err)
	require.True(t, ok)
	require.Equal(t, errorutil.HostGA_failedToReportImmediateStatus, code)
	n.Unregister()
	require.NoError(t, n.Notify(types.StatusEventArgs{}))
}
