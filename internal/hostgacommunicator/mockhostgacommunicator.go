package hostgacommunicator

import "github.com/go-kit/kit/log"

type MockHostGACommunicator struct {
	GetImmediateVMSettingsFunc func(ctx *log.Context) (*VMSettings, error)
}

func (m MockHostGACommunicator) GetImmediateVMSettings(ctx *log.Context) (*VMSettings, error) {
	return m.GetImmediateVMSettingsFunc(ctx)
}
