package hostgacommunicator

import (
	"os"
	"testing"

	"github.com/go-kit/kit/log"
	"github.com/stretchr/testify/require"
)

func Test_GetOperationUri(t *testing.T) {
	ctx := log.NewContext(log.NewSyncLogger(log.NewLogfmtLogger(os.Stdout))).With("time", log.DefaultTimestamp)
	operationName := "testOperationName"
	uri, err := getOperationUri(ctx, operationName)
	require.Nil(t, err)
	require.NotNil(t, uri)
	require.Contains(t, uri, operationName)
}
