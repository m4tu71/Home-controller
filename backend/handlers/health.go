package handlers

import (
	"encoding/json"
	"net/http"

	"backend/controller_client"
	"backend/models"
)

func HealthHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	json.NewEncoder(w).Encode(models.HealthResponse{
		OK:      true,
		Service: "backend",
		Status:  "healthy",
	})
}

func ControllerHealthHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	daemonResponse, err := controller_client.SendCommand("LED_STATUS")
	if err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)

		json.NewEncoder(w).Encode(models.ControllerHealthResponse{
			OK:       false,
			Service:  "controller-daemon",
			Status:   "unhealthy",
			DaemonOK: false,
			Error:    err.Error(),
		})
		return
	}

	json.NewEncoder(w).Encode(models.ControllerHealthResponse{
		OK:        true,
		Service:   "controller-daemon",
		Status:    "healthy",
		DaemonOK:  true,
		DaemonRaw: daemonResponse,
	})
}
