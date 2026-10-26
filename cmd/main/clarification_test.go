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
		{nil, errorutil.CustomerInput_invalidCommandLineArguments},
		{[]string{"handler"}, errorutil.CustomerInput_invalidCommandLineArguments},
		{[]string{"handler", "enable", "extra"}, errorutil.CustomerInput_invalidCommandLineArguments},
		{[]string{"handler", "invalid"}, errorutil.CustomerInput_unknownCommandLineOperation},
	} {
		_, err := parseCommand(tc.args)
		code, ok := errorutil.Code(err)
		require.True(t, ok)
		require.Equal(t, tc.code, code)
	}
	command, err := parseCommand([]string{"handler", "enable"})
	require.NoError(t, err)
	require.NotEmpty(t, command.Name)
}
