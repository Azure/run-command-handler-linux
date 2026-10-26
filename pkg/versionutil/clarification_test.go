package versionutil

import (
	"io"
	"testing"

	"github.com/Azure/run-command-handler-linux/pkg/errorutil"
	"github.com/go-kit/kit/log"
	"github.com/stretchr/testify/require"
)

func TestVersionClarification(t *testing.T) {
	_, err := ExtractFromServiceDefinition("invalid", log.NewContext(log.NewLogfmtLogger(io.Discard)))
	code, ok := errorutil.Code(err)
	require.True(t, ok)
	require.Equal(t, errorutil.Internal_invalidServiceDefinition, code)
}
