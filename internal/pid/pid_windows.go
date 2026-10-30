package pid

import (
	"errors"
	"syscall"
)

// Windows builds support mocked tests, not Linux process-group operations.
var killProcessGroup = func(int, syscall.Signal) error {
	return errors.ErrUnsupported
}
