package download

import (
	"io"
	"os"

	"github.com/Azure/azure-extension-platform/vmextension"
	"github.com/Azure/run-command-handler-linux/internal/constants"
	"github.com/go-kit/kit/log"
	"github.com/pkg/errors"
)

const (
	writeBufSize = 1024 * 8
)

// SaveTo uses given downloader to fetch the resource with retries and saves the
// given file. Directory of dst is not created by this function. If a file at
// dst exists, it will be truncated. If a new file is created, mode is used to
// set the permission bits. Written number of bytes are returned on success.
func SaveTo(ctx *log.Context, downloaders []Downloader, dst string, mode os.FileMode) (int64, *vmextension.ErrorWithClarification) {
	f, err := os.OpenFile(dst, os.O_WRONLY|os.O_TRUNC|os.O_CREATE, mode)
	if err != nil {
		return 0, vmextension.NewErrorWithClarificationPtr(constants.FileDownload_OpenFileForWriteFailure, errors.Wrapf(err, "failed to open file for writing: %s", dst))
	}
	defer f.Close()

	body, ewc := WithRetries(ctx, downloaders, ActualSleep)
	if ewc != nil {
		return 0, ewc
	}
	defer body.Close()

	n, err := io.CopyBuffer(f, body, make([]byte, writeBufSize))
	if err != nil {
		return n, vmextension.NewErrorWithClarificationPtr(constants.FileDownload_WriteFileError, errors.Wrapf(err, "failed to write to file: %s", dst))
	}

	return n, nil
}
