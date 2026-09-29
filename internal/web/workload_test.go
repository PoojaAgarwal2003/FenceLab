package web

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/PoojaAgarwal2003/FenceLab/internal/scheduler"
)

func TestWorkloadArtifactAPI(t *testing.T) {
	config := scheduler.Config{Version: scheduler.Version, Jobs: 4, Capacity: 4, Policy: scheduler.Fair,
		MaxAttempts: 2, Fault: "lost-ack-always", FaultEvery: 4, ReopenAfter: 3}
	report, err := scheduler.Run(context.Background(), filepath.Join(t.TempDir(), "run"), config)
	if err != nil {
		t.Fatal(err)
	}
	handler := Handler(log.New(io.Discard, "", 0))
	for _, tampered := range []bool{false, true} {
		if tampered {
			report.Counts.Completed++
		}
		body, err := json.Marshal(report)
		if err != nil {
			t.Fatal(err)
		}
		request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8091/api/artifacts/check", bytes.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		want := http.StatusOK
		if tampered {
			want = http.StatusBadRequest
		}
		if response.Code != want {
			t.Fatalf("got %d: %s", response.Code, response.Body.String())
		}
	}
}
