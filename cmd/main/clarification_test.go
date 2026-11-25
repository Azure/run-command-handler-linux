package main

import (
	"testing"

	"github.com/Azure/run-command-handler-linux/pkg/errorutil"
	"github.com/stretchr/testify/require"
)

func TestCommandLineClarifications(t *testing.T) {
	for _, tc := range []struct {
		args []string
		code int
	}{
		{nil, errorutil.InvalidCommandLineArguments},
		{[]string{"handler"}, errorutil.InvalidCommandLineArguments},
		{[]string{"handler", "enable", "extra"}, errorutil.InvalidCommandLineArguments},
		{[]string{"handler", "invalid"}, errorutil.UnknownCommandLineOperation},
	} {
		_, err := parseCommand(tc.args)
		code, ok := errorutil.FindClarification(err)
		require.True(t, ok)
		require.Equal(t, tc.code, code)
	}
	command, err := parseCommand([]string{"handler", "enable"})
	require.NoError(t, err)
	require.NotEmpty(t, command.Name)
}
