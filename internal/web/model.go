package web

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"mime"
	"net/http"
	"time"

	"github.com/PoojaAgarwal2003/FenceLab/internal/bridge"
	"github.com/PoojaAgarwal2003/FenceLab/internal/durable"
	"github.com/PoojaAgarwal2003/FenceLab/internal/model"
	"github.com/PoojaAgarwal2003/FenceLab/internal/scheduler"
)

func modelRequest(w http.ResponseWriter, r *http.Request, slots chan struct{}, logger *log.Logger) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "v2 operations require POST"}, logger)
		return
	}
	if r.URL.RawQuery != "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "v2 endpoints do not accept query parameters"}, logger)
		return
	}
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		writeJSON(w, http.StatusUnsupportedMediaType, map[string]string{"error": "Content-Type must be application/json"}, logger)
		return
	}
	select {
	case slots <- struct{}{}:
		defer func() { <-slots }()
	default:
		w.Header().Set("Retry-After", "1")
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "v2 laboratory is busy; retry shortly"}, logger)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	r.Body = http.MaxBytesReader(w, r.Body, model.MaxInput)
	var result any
	switch r.URL.Path {
	case "/api/artifacts/check":
		var raw json.RawMessage
		err = model.Decode(r.Body, &raw)
		if err == nil {
			result, err = checkArtifact(ctx, raw)
		}
	case "/api/v2/run":
		var scenario model.Scenario
		err = model.Decode(r.Body, &scenario)
		if err == nil {
			result, err = model.Run(ctx, scenario)
		}
	case "/api/v2/search":
		var request model.SearchRequest
		err = model.Decode(r.Body, &request)
		if err == nil {
			result, err = model.Search(ctx, request)
		}
	case "/api/v2/replay":
		var replay model.ReplayFile
		err = model.Decode(r.Body, &replay)
		if err == nil {
			result, err = model.Replay(ctx, replay)
		}
	default:
		err = fmt.Errorf("unknown v2 operation")
	}
	if err != nil {
		status := http.StatusBadRequest
		var sizeError *http.MaxBytesError
		if errors.As(err, &sizeError) {
			status = http.StatusRequestEntityTooLarge
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			status = http.StatusRequestTimeout
		}
		writeJSON(w, status, map[string]string{"error": err.Error()}, logger)
		return
	}
	writeJSON(w, http.StatusOK, result, logger)
}

func checkArtifact(ctx context.Context, raw json.RawMessage) (any, error) {
	var header struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(raw, &header); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	switch header.Version {
	case scheduler.Version:
		var report scheduler.Report
		if err := model.Decode(bytes.NewReader(raw), &report); err != nil {
			return nil, err
		}
		return report, scheduler.Check(report)
	case bridge.Version:
		var report bridge.Report
		if err := model.Decode(bytes.NewReader(raw), &report); err != nil {
			return nil, err
		}
		return report, bridge.Check(ctx, report)
	case "fencelab/durability-v1":
		var report durable.CrashReport
		if err := model.Decode(bytes.NewReader(raw), &report); err != nil {
			return nil, err
		}
		return report, durable.CheckCrashReport(report)
	default:
		return nil, fmt.Errorf("unsupported artifact version")
	}
}
