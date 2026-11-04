package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"backend/models"
	"backend/repositories"
	"backend/services"
)

type dataHealthScannerFixture struct {
	issues []models.DataHealthIssue
	err    error
}

func (f dataHealthScannerFixture) Scan() ([]models.DataHealthIssue, error) { return f.issues, f.err }

func TestDataHealthHandlerEmptyAndFailures(t *testing.T) {
	for _, tc := range []struct {
		name         string
		handler      http.Handler
		method, path string
		status       int
		body         string
	}{
		{"empty", NewDataHealthHandler(dataHealthScannerFixture{}), "GET", DataHealthRoute, 200, `"issues":[]`},
		{"service failure", NewDataHealthHandler(dataHealthScannerFixture{err: errors.New("private filesystem detail")}), "GET", DataHealthRoute, 503, "data health scan failed"},
		{"unavailable", NewDataHealthHandler(nil), "GET", DataHealthRoute, 503, "data health unavailable"},
		{"method", NewDataHealthHandler(dataHealthScannerFixture{}), "POST", DataHealthRoute, 405, "method not allowed"},
		{"query", NewDataHealthHandler(dataHealthScannerFixture{}), "GET", DataHealthRoute + "?all=true", 400, "query parameters are not supported"},
		{"wrong path", NewDataHealthHandler(dataHealthScannerFixture{}), "GET", DataHealthRoute + "/other", 404, "route not found"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			tc.handler.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
			if w.Code != tc.status || !strings.Contains(w.Body.String(), tc.body) {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
			if strings.Contains(w.Body.String(), "private filesystem detail") {
				t.Fatal("internal error leaked")
			}
		})
	}
}

func TestDataHealthRouteInvalidDataFixture(t *testing.T) {
	root := filepath.Join(t.TempDir(), "data")
	if err := os.MkdirAll(filepath.Join(root, "ir-signals"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "ir-signals", "broken.json"), []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.Handle(DataHealthRoute, NewDataHealthHandler(services.NewDataHealthService(repositories.NewDataHealthRepository(root))))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, DataHealthRoute, nil))
	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var response struct {
		OK     bool             `json:"ok"`
		Issues []map[string]any `json:"issues"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !response.OK || len(response.Issues) != 1 || response.Issues[0]["id"] != "invalid_json:ir-signals/broken.json" {
		t.Fatalf("response=%+v", response)
	}
	if _, exists := response.Issues[0]["canonical_path"]; exists {
		t.Fatal("absolute path leaked")
	}
	if response.Issues[0]["hash"] == "" {
		t.Fatal("hash missing")
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("cache control missing")
	}
}
