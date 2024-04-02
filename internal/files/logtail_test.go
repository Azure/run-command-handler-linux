package files

import (
	"bytes"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_tailFile_notFound(t *testing.T) {
	b, err := TailFile("/non/existing/path", 1024)
	require.Nil(t, err)
	require.Len(t, b, 0)
}

func Test_tailFile_openError(t *testing.T) {
	tf := tempFile(t)
	defer os.RemoveAll(tf)

	require.Nil(t, os.Chmod(tf, 0333)) // no read
	_, err := TailFile(tf, 1024)
	require.NotNil(t, err)
	require.Regexp(t, `^error opening file:`, err.Error())
}

func Test_tailFile(t *testing.T) {
	tf := tempFile(t)
	defer os.RemoveAll(tf)

	in := bytes.Repeat([]byte("0123456789"), 10)
	require.Nil(t, os.WriteFile(tf, in, 0666))

	// max=0
	b, err := TailFile(tf, 0)
	require.Nil(t, err)
	require.Len(t, b, 0)

	// max < size
	b, err = TailFile(tf, 5)
	require.Nil(t, err)
	require.EqualValues(t, []byte("56789"), b)

	// max==size
	b, err = TailFile(tf, int64(len(in)))
	require.Nil(t, err)
	require.EqualValues(t, in, b)

	// max>=size
	b, err = TailFile(tf, int64(len(in)+1000))
	require.Nil(t, err)
	require.EqualValues(t, in, b)
}

func Test_getFileFromPosition(t *testing.T) {
	tf := tempFile(t)
	defer os.RemoveAll(tf)

	// size = 100
	in := bytes.Repeat([]byte("0123456789"), 10)
	require.Nil(t, os.WriteFile(tf, in, 0666))

	// position = 0
	b, err := GetFileFromPosition(tf, 0)
	require.Nil(t, err)
	require.EqualValues(t, in, b)

	// position(90) < size(100)
	b, err = GetFileFromPosition(tf, 90)
	require.Nil(t, err)
	require.EqualValues(t, []byte("0123456789"), b)

	// position(100)>=size(100)
	b, err = GetFileFromPosition(tf, 100)
	require.Nil(t, err)
	require.Len(t, b, 0)
}

func tempFile(t *testing.T) string {
	f, err := os.CreateTemp("", "")
	require.Nil(t, err, "error creating test file")
	defer f.Close()
	return f.Name()
}
