package web

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"time"

	"github.com/PoojaAgarwal2003/FenceLab/internal/model"
	"github.com/PoojaAgarwal2003/FenceLab/internal/sim"
)

//go:embed static/*
var assets embed.FS

func loopbackAddress(address string) bool {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return false
	}
	ip, err := netip.ParseAddr(host)
	if err != nil || !ip.IsLoopback() {
		return false
	}
	number, err := strconv.Atoi(port)
	return err == nil && number >= 0 && number <= 65535
}

func Handler(logger *log.Logger) http.Handler {
	slots := make(chan struct{}, 4)
	modelSlots := make(chan struct{}, 1)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; connect-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'none'")
		if !loopbackAddress(r.Host) {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "use a numeric loopback host and port"}, logger)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" && origin != "http://"+r.Host {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "cross-origin requests are not allowed"}, logger)
			return
		}
		if site := r.Header.Get("Sec-Fetch-Site"); site == "cross-site" {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "cross-site requests are not allowed"}, logger)
			return
		}
		if r.URL.Path == "/api/v2/run" || r.URL.Path == "/api/v2/search" || r.URL.Path == "/api/v2/replay" {
			modelRequest(w, r, modelSlots, logger)
			return
		}
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "only GET is supported"}, logger)
			return
		}
		switch r.URL.Path {
		case "/api/v2/examples":
			writeJSON(w, http.StatusOK, model.Examples(), logger)
		case "/api/health":
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "model": sim.ModelVersion}, logger)
		case "/api/compare":
			c, err := parseConfig(r.URL.RawQuery)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()}, logger)
				return
			}
			select {
			case slots <- struct{}{}:
				defer func() { <-slots }()
			default:
				w.Header().Set("Retry-After", "1")
				writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "all simulation slots are busy; retry shortly"}, logger)
				return
			}
			if err := r.Context().Err(); err != nil {
				logger.Printf("comparison canceled: %v", err)
				return
			}
			results, err := sim.Compare(c)
			if err != nil {
				logger.Printf("comparison failed: %v", err)
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "simulation failed"}, logger)
				return
			}
			writeJSON(w, http.StatusOK, results, logger)
		case "/", "/app.js", "/network.js", "/style.css":
			name, contentType := "index.html", "text/html; charset=utf-8"
			if r.URL.Path == "/app.js" {
				name, contentType = "app.js", "text/javascript; charset=utf-8"
			}
			if r.URL.Path == "/network.js" {
				name, contentType = "network.js", "text/javascript; charset=utf-8"
			}
			if r.URL.Path == "/style.css" {
				name, contentType = "style.css", "text/css; charset=utf-8"
			}
			body, err := assets.ReadFile("static/" + name)
			if err != nil {
				logger.Printf("embedded asset %s: %v", name, err)
				http.Error(w, "asset unavailable", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", contentType)
			if _, err := w.Write(body); err != nil {
				logger.Printf("writing asset %s: %v", name, err)
			}
		default:
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"}, logger)
		}
	})
}

func parseConfig(raw string) (sim.Config, error) {
	c := sim.DefaultConfig()
	if len(raw) > 256 {
		return c, fmt.Errorf("query is too long")
	}
	values, err := url.ParseQuery(raw)
	if err != nil {
		return c, fmt.Errorf("invalid query encoding")
	}
	for key, entries := range values {
		if len(entries) != 1 || entries[0] == "" {
			return c, fmt.Errorf("%s must have exactly one nonempty value", key)
		}
		value := entries[0]
		switch key {
		case "scenario":
			c.Scenario = sim.Scenario(value)
		case "seed":
			c.Seed, err = strconv.ParseInt(value, 10, 64)
			if err != nil || c.Seed < -9007199254740991 || c.Seed > 9007199254740991 {
				return c, fmt.Errorf("seed must be a JavaScript-safe integer")
			}
		case "lease_ms":
			c.LeaseMS, err = strconv.Atoi(value)
			if err != nil {
				return c, fmt.Errorf("lease_ms must be an integer")
			}
		default:
			return c, fmt.Errorf("unknown query field %q", key)
		}
	}
	return c, c.Validate()
}

func writeJSON(w http.ResponseWriter, status int, value any, logger *log.Logger) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		logger.Printf("writing JSON response: %v", err)
	}
}

func Serve(ctx context.Context, address string, output io.Writer) error {
	if !loopbackAddress(address) {
		return fmt.Errorf("listen must be a numeric loopback address and port, such as 127.0.0.1:8091")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return err
	}
	logger := log.New(output, "fencelab: ", 0)
	server := &http.Server{
		Handler: Handler(logger), ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout: 5 * time.Second, WriteTimeout: 10 * time.Second,
		IdleTimeout: 30 * time.Second, MaxHeaderBytes: 8192,
	}
	if _, err := fmt.Fprintf(output, "FenceLab ready at http://%s (local simulation only)\n", listener.Addr()); err != nil {
		if closeErr := listener.Close(); closeErr != nil {
			return errors.Join(err, closeErr)
		}
		return err
	}
	finished := make(chan error, 1)
	go func() { finished <- server.Serve(listener) }()
	select {
	case err := <-finished:
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return errors.Join(err, server.Close())
		}
		err := <-finished
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
}
