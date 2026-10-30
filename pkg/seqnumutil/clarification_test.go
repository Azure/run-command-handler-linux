package seqnumutil

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Azure/run-command-handler-linux/pkg/errorutil"
	"github.com/stretchr/testify/require"
)

func TestSequenceClarifications(t *testing.T) {
	dir, err := os.MkdirTemp(".", ".clarification-")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })
	assertCode := func(err error, want int) {
		t.Helper()
		code, ok := errorutil.Code(err)
		require.True(t, ok, "%v", err)
		require.Equal(t, want, code)
	}
	_, err = FindSequenceNumberFromConfig(dir, ".settings", "[")
	assertCode(err, errorutil.Internal_failedToReadSequenceConfigurationDirectory)
	_, err = FindSequenceNumberFromConfig(dir, ".settings", "test")
	assertCode(err, errorutil.Internal_noSequenceConfigurationFiles)
	assertCode(SaveSeqNum(dir, 1), errorutil.Os_FailedToWriteSequenceNumber)
	_, err = IsSmallerThan(dir, 1)
	assertCode(err, errorutil.Os_FailedToReadSequenceNumber)
	file := filepath.Join(dir, "sequence")
	require.NoError(t, os.WriteFile(file, []byte("bad"), 0600))
	_, err = IsSmallerThan(file, 1)
	assertCode(err, errorutil.Os_InvalidStoredSequenceNumber)
	require.NoError(t, SaveSeqNum(file, 1))
	smaller, err := IsSmallerThan(file, 2)
	require.NoError(t, err)
	require.True(t, smaller)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "test.bad.settings"), nil, 0600))
	_, err = FindSequenceNumberFromConfig(dir, ".settings", "test")
	assertCode(err, errorutil.Internal_noSequenceConfigurationFiles)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "test.9.settings"), nil, 0600))
	seq, err := FindSequenceNumberFromConfig(dir, ".settings", "test")
	require.NoError(t, err)
	require.Equal(t, 9, seq)
}
