package cluster

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"mime"
	"net"
	"net/http"
	"net/url"
	"slices"
	"time"

	"github.com/PoojaAgarwal2003/FenceLab/internal/model"
)

func Handler(node *Node, logger *log.Logger) http.Handler {
	slots := make(chan struct{}, 16)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		write := func(code int, reply Reply) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(code)
			if err := json.NewEncoder(w).Encode(reply); err != nil {
				logger.Printf("cluster response write: %v", err)
			}
		}
		if r.TLS == nil || len(r.TLS.VerifiedChains) == 0 || len(r.TLS.PeerCertificates) == 0 {
			write(http.StatusUnauthorized, bad("verified client certificate required"))
			return
		}
		cert := r.TLS.PeerCertificates[0]
		admin := slices.Contains(cert.Subject.OrganizationalUnit, "admin")
		worker := slices.Contains(cert.Subject.OrganizationalUnit, "worker")
		if !admin && !worker {
			write(http.StatusForbidden, bad("certificate has no API role"))
			return
		}
		if r.Header.Get("Origin") != "" || r.Header.Get("Sec-Fetch-Site") != "" {
			write(http.StatusForbidden, bad("cluster API does not accept browser-origin requests"))
			return
		}
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
		default:
			w.Header().Set("Retry-After", "1")
			write(http.StatusTooManyRequests, Reply{Status: "busy", Error: "cluster API admission full"})
			return
		}
		path := r.URL.Path
		if path == "/health/live" || path == "/health/ready" {
			if r.Method != http.MethodGet || r.URL.RawQuery != "" {
				write(http.StatusBadRequest, bad("health requires GET without query"))
				return
			}
			if path == "/health/ready" {
				if err := node.Raft.VerifyLeader().Error(); err != nil {
					write(http.StatusServiceUnavailable, Reply{Status: "unavailable", Error: err.Error()})
					return
				}
			}
			write(http.StatusOK, Reply{Status: "alive"})
			return
		}
		if path == "/v1/status" {
			query, err := queryKey(r)
			if !admin {
				write(http.StatusForbidden, bad("admin certificate required"))
				return
			}
			if err != nil {
				write(http.StatusBadRequest, bad(err.Error()))
				return
			}
			reply, err := node.Query(query)
			if err != nil {
				write(http.StatusServiceUnavailable, Reply{Status: "unavailable", Error: err.Error()})
				return
			}
			write(replyCode(reply), reply)
			return
		}
		operation := ""
		switch path {
		case "/v1/enqueue":
			if admin {
				operation = "enqueue"
			}
		case "/v1/claim":
			if worker {
				operation = "claim"
			}
		case "/v1/complete":
			if worker {
				operation = "complete"
			}
		default:
			write(http.StatusNotFound, bad("unknown cluster endpoint"))
			return
		}
		if operation == "" {
			write(http.StatusForbidden, bad("operation not permitted for certificate role"))
			return
		}
		media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if r.Method != http.MethodPost || r.URL.RawQuery != "" || err != nil || media != "application/json" {
			write(http.StatusBadRequest, bad("POST application/json without query required"))
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 8192)
		var command Command
		if err := model.Decode(r.Body, &command); err != nil {
			write(http.StatusBadRequest, bad(err.Error()))
			return
		}
		if command.Operation != "" && command.Operation != operation {
			write(http.StatusBadRequest, bad("operation conflicts with route"))
			return
		}
		command.Operation = operation
		if worker {
			if command.Worker != "" && command.Worker != cert.Subject.CommonName {
				write(http.StatusForbidden, bad("worker identity must match certificate"))
				return
			}
			command.Worker = cert.Subject.CommonName
		}
		reply, err := node.Apply(command)
		if err != nil {
			write(http.StatusServiceUnavailable, Reply{Status: "unavailable", Error: err.Error()})
			return
		}
		write(replyCode(reply), reply)
	})
}

func queryKey(r *http.Request) (string, error) {
	if r.Method != http.MethodGet || len(r.URL.RawQuery) > 128 {
		return "", fmt.Errorf("bounded GET query required")
	}
	values, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return "", err
	}
	if len(values) == 0 {
		return "", nil
	}
	key := values.Get("key")
	if len(values) != 1 || len(values["key"]) != 1 || !identifier.MatchString(key) {
		return "", fmt.Errorf("only one valid key query is accepted")
	}
	return key, nil
}

func replyCode(r Reply) int {
	switch r.Status {
	case "invalid":
		return http.StatusBadRequest
	case "full":
		return http.StatusTooManyRequests
	case "conflict", "stale", "busy":
		return http.StatusConflict
	case "missing":
		return http.StatusNotFound
	default:
		return http.StatusOK
	}
}

func StartHTTP(address string, config *tls.Config, handler http.Handler, logger *log.Logger) (*http.Server, <-chan error, error) {
	if config == nil || config.ClientAuth != tls.RequireAndVerifyClientCert || config.MinVersion < tls.VersionTLS13 {
		return nil, nil, fmt.Errorf("API requires TLS 1.3 mutual authentication")
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return nil, nil, err
	}
	server := &http.Server{Handler: handler, TLSConfig: config, ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 8192, ErrorLog: logger}
	done := make(chan error, 1)
	go func() {
		err := server.Serve(tls.NewListener(listener, config))
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		done <- err
	}()
	return server, done, nil
}
