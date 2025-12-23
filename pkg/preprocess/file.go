package preprocess

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Azure/azure-extension-platform/vmextension"
	"github.com/Azure/run-command-handler-linux/internal/constants"
	"github.com/pkg/errors"
)

const peekLen = 64 // look at first N bytes to figure out if it has shebang

// textExtensions is predefined list of script and text
// file extensions.
var textExtensions = []string{
	".sh",
	".txt",
	".py",
	".pl",
}

// IsTextFile is a best effort to determine if a file
// is a script file (with a known file extension) or a
// file that starts with a shebang (!#)
func IsTextFile(path string) (bool, error) {
	if hasTextExtension(path) {
		return true, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return false, vmextension.NewErrorWithClarification(constants.Internal_FailedToOpenFileForReading, errors.Wrap(err, "failed to open file"))
	}
	defer f.Close()
	b := make([]byte, peekLen)
	_, err = f.Read(b)
	if err != nil && err != io.EOF {
		return false, vmextension.NewErrorWithClarification(constants.Internal_FailedToReadFile, errors.Wrap(err, "failed to read file"))
	}
	return hasShebang(b), nil
}

// hasShebang checks if provided file contents start with #! characters
// once the BOM and space characters are trimmed from the beginning.
func hasShebang(b []byte) bool {
	b = RemoveBOM(b)
	return bytes.HasPrefix(b, []byte{'#', '!'})
}

// hasTextExtension is a best effort to determine if a
// file's extension is incidator of a text or script contents.
func hasTextExtension(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	for _, v := range textExtensions {
		if ext == v {
			return true
		}
	}
	return false
}
