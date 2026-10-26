package pid

import (
	"errors"
	"syscall"
	"testing"

	"github.com/Azure/run-command-handler-linux/pkg/errorutil"
	"github.com/stretchr/testify/require"
)

func TestWindowsProcessGroupUnsupported(t *testing.T) {
	// The platform shim returns a raw unsupported-operation sentinel; KillPreviousExtensionCommand classifies it.
	err := killProcessGroup(-123, syscall.SIGKILL)
	require.ErrorIs(t, err, errors.ErrUnsupported)
	_, classified := errorutil.Code(err)
	require.False(t, classified)
}
