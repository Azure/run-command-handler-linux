package types

// HandlerEnvironment describes the handler environment configuration presented
// to the extension handler by the Azure Linux Guest Agent.
type HandlerEnvironment struct {
	Version            float64 `json:"version"`
	Name               string  `json:"name"`
	HandlerEnvironment struct {
		HeartbeatFile string `json:"heartbeatFile"`
		StatusFolder  string `json:"statusFolder"`
		ConfigFolder  string `json:"configFolder"`
		LogFolder     string `json:"logFolder"`
	}
}
