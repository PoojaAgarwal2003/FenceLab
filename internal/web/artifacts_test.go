package web

import (
	"bytes"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestArtifactConsistencyAPI(t *testing.T) {
	handler := Handler(log.New(io.Discard, "", 0))
	for _, name := range []string{"durability", "process-eager", "process-barrier", "process-lost-result"} {
		body, err := os.ReadFile(filepath.Join("..", "..", "docs", "evidence", name+".json"))
		if err != nil {
			t.Fatal(err)
		}
		request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8091/api/artifacts/check", bytes.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", name, response.Code, response.Body.String())
		}
		tampered := strings.Replace(string(body), `"safe": true`, `"safe": false`, 1)
		if name == "process-eager" {
			tampered = strings.Replace(string(body), `"matches_model": true`, `"matches_model": false`, 1)
		}
		request = httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8091/api/artifacts/check", strings.NewReader(tampered))
		request.Header.Set("Content-Type", "application/json")
		response = httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("accepted tampered %s: %d", name, response.Code)
		}
	}
	for _, body := range []string{`{"version":"unknown"}`, `{"version":"a","version":"b"}`, strings.Repeat("x", 65537)} {
		request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8091/api/artifacts/check", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code < 400 {
			t.Fatal("accepted invalid artifact")
		}
	}
}
