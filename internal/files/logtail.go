package files

import (
	"io"
	"os"

	"github.com/Azure/run-command-handler-linux/pkg/errorutil"
	"github.com/pkg/errors"
)

type readableFile interface {
	io.Reader
	io.Seeker
	Stat() (os.FileInfo, error)
	Close() error
}

var openReadableFile = func(path string) (readableFile, error) { return os.Open(path) }

// TailFile returns the last max bytes (or the entire file if the file size is
// smaller than max) from the file at path. If the file does not exist, it
// returns a nil slice and no error.
func TailFile(path string, max int64) ([]byte, error) {
	f, err := openReadableFile(path)
	if err != nil && os.IsNotExist(err) {
		return nil, nil
	} else if err != nil {
		return nil, errorutil.WithCode(errorutil.Os_FailedToOpenTailFile, errors.Wrap(err, "error opening file"))
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return nil, errorutil.WithCode(errorutil.Os_FailedToStatTailFile, errors.Wrap(err, "error retrieving file info"))
	}
	size := fi.Size()

	n := max
	if size < n {
		n = size
	}
	_, err = f.Seek(-n, io.SeekEnd)
	if err != nil {
		return nil, errorutil.WithCode(errorutil.Os_FailedToSeekTailFile, errors.Wrapf(err, "error seeking file: offset=%d whence=%v", n, io.SeekEnd))
	}

	b, err := io.ReadAll(io.LimitReader(f, max))
	return b, errorutil.WithCode(errorutil.Os_FailedToReadTailFile, errors.Wrap(err, "error reading from file"))
}

func GetFileFromPosition(path string, position int64) ([]byte, error) {
	f, err := openReadableFile(path)
	if err != nil && os.IsNotExist(err) {
		return nil, nil
	} else if err != nil {
		return nil, errorutil.WithCode(errorutil.Os_FailedToOpenOutputFile, errors.Wrap(err, "error opening file"))
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return nil, errorutil.WithCode(errorutil.Os_FailedToStatOutputFile, errors.Wrap(err, "error retrieving file info"))
	}
	size := fi.Size()
	if position >= size {
		// No content after position
		return make([]byte, 0), nil
	}

	_, err = f.Seek(position, io.SeekStart)
	if err != nil {
		return nil, errorutil.WithCode(errorutil.Os_FailedToSeekOutputFile, errors.Wrapf(err, "error seeking file: %s, offset=%d", path, position))
	}

	b, err := io.ReadAll(io.LimitReader(f, size-position))
	return b, errorutil.WithCode(errorutil.Os_FailedToReadOutputFile, errors.Wrap(err, "error reading from file: "+path))
}
