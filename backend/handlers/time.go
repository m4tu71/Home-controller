package handlers

import (
	"encoding/json"
	"net/http"
	"time"
)

func TimeHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	now := time.Now()

	json.NewEncoder(w).Encode(map[string]string{
		"date":     now.Format("2006-01-02"),
		"time":     now.Format("15:04:05"),
		"datetime": now.Format("2006-01-02 15:04:05"),
	})
}
