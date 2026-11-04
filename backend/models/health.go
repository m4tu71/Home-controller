package models

type HealthResponse struct {
	OK      bool   `json:"ok"`
	Service string `json:"service"`
	Status  string `json:"status"`
}

type ControllerHealthResponse struct {
	OK        bool   `json:"ok"`
	Service   string `json:"service"`
	Status    string `json:"status"`
	DaemonOK  bool   `json:"daemonOk"`
	DaemonRaw string `json:"daemonRaw,omitempty"`
	Error     string `json:"error,omitempty"`
}
