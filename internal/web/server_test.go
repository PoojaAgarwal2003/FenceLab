package web

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/PoojaAgarwal2003/FenceLab/internal/sim"
)

func TestComparisonMatchesEngine(t *testing.T) {
	h := Handler(log.New(io.Discard, "", 0))
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8091/api/compare?seed=7&scenario=lost-ack&lease_ms=100", nil)
	out := httptest.NewRecorder()
	h.ServeHTTP(out, req)
	if out.Code != http.StatusOK {
		t.Fatal(out.Body.String())
	}
	var got []sim.Result
	if err := json.Unmarshal(out.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	c := sim.DefaultConfig()
	c.Scenario = sim.LostAck
	want, _ := sim.Compare(c)
	gotJSON, _ := json.Marshal(got)
	wantJSON, _ := json.Marshal(want)
	if string(gotJSON) != string(wantJSON) {
		t.Fatal("HTTP results differ from CLI model")
	}
}

func TestHTTPBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, method, target, origin string
		code                         int
	}{
		{"root", "GET", "http://127.0.0.1:8091/", "", 200},
		{"script", "GET", "http://127.0.0.1:8091/app.js", "", 200},
		{"css", "GET", "http://127.0.0.1:8091/style.css", "", 200},
		{"health", "GET", "http://127.0.0.1:8091/api/health", "", 200},
		{"ipv6", "GET", "http://[::1]:8091/api/health", "", 200},
		{"post", "POST", "http://127.0.0.1:8091/api/compare", "", 405},
		{"rebinding", "GET", "http://evil.example:8091/api/compare", "", 403},
		{"origin", "GET", "http://127.0.0.1:8091/api/compare", "https://evil.example", 403},
		{"same origin", "GET", "http://127.0.0.1:8091/api/compare", "http://127.0.0.1:8091", 200},
		{"unknown path", "GET", "http://127.0.0.1:8091/secrets", "", 404},
		{"invalid scenario", "GET", "http://127.0.0.1:8091/api/compare?scenario=oops", "", 400},
		{"empty value", "GET", "http://127.0.0.1:8091/api/compare?seed=", "", 400},
		{"duplicate", "GET", "http://127.0.0.1:8091/api/compare?seed=1&seed=2", "", 400},
		{"unknown field", "GET", "http://127.0.0.1:8091/api/compare?oops=1", "", 400},
		{"invalid encoding", "GET", "http://127.0.0.1:8091/api/compare?seed=%zz", "", 400},
		{"long query", "GET", "http://127.0.0.1:8091/api/compare?seed=" + strings.Repeat("1", 257), "", 400},
		{"unsafe integer", "GET", "http://127.0.0.1:8091/api/compare?seed=9007199254740992", "", 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, tc.target, nil)
			r.Header.Set("Origin", tc.origin)
			w := httptest.NewRecorder()
			Handler(log.New(io.Discard, "", 0)).ServeHTTP(w, r)
			if w.Code != tc.code {
				t.Fatalf("status %d want %d: %s", w.Code, tc.code, w.Body.String())
			}
			if w.Header().Get("Content-Security-Policy") == "" || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("missing response protection")
			}
		})
	}
}

func TestCrossSiteRequestsRejected(t *testing.T) {
	r := httptest.NewRequest("GET", "http://127.0.0.1:8091/api/compare", nil)
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	w := httptest.NewRecorder()
	Handler(log.New(io.Discard, "", 0)).ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatal("cross-site request accepted")
	}
}

func TestInvalidListenersAndCanceledStart(t *testing.T) {
	for _, address := range []string{"0.0.0.0:8091", "localhost:8091", "[::]:8091", "127.0.0.1:99999", "127.0.0.1", "192.168.1.1:8091"} {
		if err := Serve(context.Background(), address, io.Discard); err == nil {
			t.Fatalf("invalid listener accepted: %s", address)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Serve(ctx, "127.0.0.1:0", io.Discard); err == nil {
		t.Fatal("canceled server started")
	}
}

type gatedWriter struct {
	*httptest.ResponseRecorder
	started chan struct{}
	release <-chan struct{}
}

func (w *gatedWriter) Write(p []byte) (int, error) {
	close(w.started)
	<-w.release
	return w.ResponseRecorder.Write(p)
}

func TestAdmissionRejectsOverloadAndRecovers(t *testing.T) {
	h := Handler(log.New(io.Discard, "", 0))
	release := make(chan struct{})
	finished := make(chan struct{}, 4)
	released := false
	// Cleanup also releases held handlers if an assertion fails.
	defer func() {
		if !released {
			close(release)
			for i := 0; i < 4; i++ {
				<-finished
			}
		}
	}()
	starts := make([]chan struct{}, 4)
	for i := range starts {
		starts[i] = make(chan struct{})
		w := &gatedWriter{ResponseRecorder: httptest.NewRecorder(), started: starts[i], release: release}
		go func() {
			defer func() { finished <- struct{}{} }()
			h.ServeHTTP(w, httptest.NewRequest("GET", "http://127.0.0.1:8091/api/compare", nil))
		}()
	}
	for _, started := range starts {
		select {
		case <-started:
		case <-time.After(10 * time.Second):
			t.Fatal("request did not reach held response")
		}
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "http://127.0.0.1:8091/api/compare", nil))
	if w.Code != http.StatusTooManyRequests || w.Header().Get("Retry-After") != "1" {
		t.Fatalf("overload response = %d: %s", w.Code, w.Body.String())
	}
	health := httptest.NewRecorder()
	h.ServeHTTP(health, httptest.NewRequest("GET", "http://127.0.0.1:8091/api/health", nil))
	if health.Code != http.StatusOK {
		t.Fatal("health endpoint blocked by comparison saturation")
	}
	close(release)
	released = true
	for i := 0; i < 4; i++ {
		<-finished
	}
	recovered := httptest.NewRecorder()
	h.ServeHTTP(recovered, httptest.NewRequest("GET", "http://127.0.0.1:8091/api/compare", nil))
	if recovered.Code != http.StatusOK {
		t.Fatal("admission did not recover after requests finished")
	}
}

type readyWriter struct{ ready chan string }

func (w readyWriter) Write(p []byte) (int, error) {
	if strings.HasPrefix(string(p), "FenceLab ready at ") {
		w.ready <- strings.Fields(string(p))[3]
	}
	return len(p), nil
}

func TestServerStartsAndShutsDown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ready, finished := make(chan string, 1), make(chan error, 1)
	go func() { finished <- Serve(ctx, "127.0.0.1:0", readyWriter{ready: ready}) }()
	var address string
	select {
	case address = <-ready:
	case err := <-finished:
		t.Fatalf("server failed before ready: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("server did not start")
	}
	client := &http.Client{Timeout: 5 * time.Second}
	response, err := client.Get(address + "/api/health")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("health status = %d", response.StatusCode)
	}
	cancel()
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("server did not shut down")
	}
}
