package web

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/PoojaAgarwal2003/FenceLab/internal/model"
)

func modelPOST(path string, body []byte) *http.Request {
	r := httptest.NewRequest("POST", "http://127.0.0.1:8091"+path, bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	return r
}

func TestV2APIMatchesEngineAndReplay(t *testing.T) {
	h := Handler(log.New(io.Discard, "", 0))
	scenario := model.Example("eager")
	body, _ := json.Marshal(scenario)
	out := httptest.NewRecorder()
	h.ServeHTTP(out, modelPOST("/api/v2/run", body))
	var result model.Result
	if out.Code != 200 || json.Unmarshal(out.Body.Bytes(), &result) != nil {
		t.Fatal(out.Code, out.Body.String())
	}
	want, _ := model.Run(context.Background(), scenario)
	if !reflect.DeepEqual(want, result) {
		t.Fatal("API output differs from model")
	}
	body, _ = json.Marshal(model.SearchRequest{Scenario: scenario, Bounds: model.Bounds{MaxStates: 2000, MaxDepth: 80}})
	out = httptest.NewRecorder()
	h.ServeHTTP(out, modelPOST("/api/v2/search", body))
	var report model.SearchReport
	if out.Code != 200 || json.Unmarshal(out.Body.Bytes(), &report) != nil || report.Witness == nil {
		t.Fatal("API search did not find witness", out.Body.String())
	}
	body, _ = json.Marshal(report.Witness)
	out = httptest.NewRecorder()
	h.ServeHTTP(out, modelPOST("/api/v2/replay", body))
	var replayed model.Result
	if out.Code != 200 || json.Unmarshal(out.Body.Bytes(), &replayed) != nil || !reflect.DeepEqual(replayed, *report.Counterexample) {
		t.Fatal("API replay differs")
	}
}

func TestV2InputAndOriginBoundaries(t *testing.T) {
	body, _ := json.Marshal(model.Example("barrier"))
	for _, tc := range []struct {
		name string
		edit func(*http.Request)
		code int
	}{
		{"get", func(r *http.Request) { r.Method = "GET" }, 405},
		{"media", func(r *http.Request) { r.Header.Set("Content-Type", "text/plain") }, 415},
		{"query", func(r *http.Request) { r.URL.RawQuery = "extra=1" }, 400},
		{"cross-site", func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "cross-site") }, 403},
		{"origin", func(r *http.Request) { r.Header.Set("Origin", "http://attacker.example") }, 403},
		{"host", func(r *http.Request) { r.Host = "attacker.example:8091" }, 403},
		{"oversized", func(r *http.Request) { r.Body = io.NopCloser(strings.NewReader(strings.Repeat(" ", model.MaxInput+1))) }, 413},
		{"malformed", func(r *http.Request) { r.Body = io.NopCloser(strings.NewReader("{")) }, 400},
		{"duplicate", func(r *http.Request) {
			r.Body = io.NopCloser(strings.NewReader(`{"version":"fencelab/v2","version":"fencelab/v2"}`))
		}, 400},
		{"canceled", func(r *http.Request) {
			ctx, cancel := context.WithCancel(r.Context())
			cancel()
			*r = *r.WithContext(ctx)
		}, 408},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := modelPOST("/api/v2/run", body)
			tc.edit(r)
			out := httptest.NewRecorder()
			Handler(log.New(io.Discard, "", 0)).ServeHTTP(out, r)
			if out.Code != tc.code || !strings.Contains(out.Body.String(), `"error"`) {
				t.Fatalf("got %d: %s", out.Code, out.Body.String())
			}
		})
	}
}

func TestV2AdmissionAndExamples(t *testing.T) {
	body, _ := json.Marshal(model.Example("barrier"))
	slots := make(chan struct{}, 1)
	slots <- struct{}{}
	out := httptest.NewRecorder()
	logger := log.New(io.Discard, "", 0)
	modelRequest(out, modelPOST("/api/v2/run", body), slots, logger)
	if out.Code != 429 || out.Header().Get("Retry-After") != "1" {
		t.Fatal("saturated v2 request not rejected")
	}
	<-slots
	out = httptest.NewRecorder()
	modelRequest(out, modelPOST("/api/v2/run", body), slots, logger)
	if out.Code != 200 || len(slots) != 0 {
		t.Fatal("admission failed to recover")
	}
	out = httptest.NewRecorder()
	Handler(logger).ServeHTTP(out, httptest.NewRequest("GET", "http://127.0.0.1:8091/api/v2/examples", nil))
	var examples map[string]model.Scenario
	if out.Code != 200 || json.Unmarshal(out.Body.Bytes(), &examples) != nil || len(examples) != 4 {
		t.Fatal("scenario examples missing")
	}
}
