package cluster

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Client struct {
	endpoints []string
	http      *http.Client
	transport *http.Transport
}

var ErrUnavailable = errors.New("no writable endpoint")

func NewClient(endpoints []string, config *tls.Config) (*Client, error) {
	if len(endpoints) < 1 || len(endpoints) > 3 || config == nil ||
		config.InsecureSkipVerify || config.MinVersion < tls.VersionTLS13 ||
		config.RootCAs == nil || len(config.Certificates) == 0 {
		return nil, fmt.Errorf("one to three HTTPS endpoints and TLS credentials required")
	}
	clean := make([]string, 0, len(endpoints))
	for _, endpoint := range endpoints {
		u, err := url.Parse(endpoint)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
			return nil, fmt.Errorf("invalid HTTPS endpoint")
		}
		clean = append(clean, strings.TrimSuffix(endpoint, "/"))
	}
	transport := &http.Transport{TLSClientConfig: config, TLSHandshakeTimeout: 2 * time.Second, ResponseHeaderTimeout: 3 * time.Second, MaxConnsPerHost: 4, MaxIdleConnsPerHost: 3}
	client := &http.Client{Transport: transport, Timeout: 4 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
		return fmt.Errorf("cluster clients do not follow redirects")
	}}
	return &Client{endpoints: clean, http: client, transport: transport}, nil
}

func (c *Client) Close() { c.transport.CloseIdleConnections() }

// Call retries only the configured endpoints, never a server-provided redirect.
// Callers must reuse keys/request IDs after ambiguous transport failures.
func (c *Client) Call(ctx context.Context, operation string, command Command) (Reply, error) {
	path := "/v1/" + operation
	method := http.MethodPost
	switch operation {
	case "status":
		method = http.MethodGet
		if command.Key != "" {
			path += "?key=" + url.QueryEscape(command.Key)
		}
	case "health":
		method = http.MethodGet
		path = "/health/live"
	case "ready":
		method = http.MethodGet
		path = "/health/ready"
	case "enqueue", "claim", "complete":
	default:
		return Reply{}, fmt.Errorf("unknown client operation")
	}
	data, err := json.Marshal(command)
	if err != nil {
		return Reply{}, err
	}
	var last error
	for _, endpoint := range c.endpoints {
		request, err := http.NewRequestWithContext(ctx, method, endpoint+path, bytes.NewReader(data))
		if err != nil {
			return Reply{}, err
		}
		request.Header.Set("Content-Type", "application/json")
		response, err := c.http.Do(request)
		if err != nil {
			if ctx.Err() != nil {
				return Reply{}, ctx.Err()
			}
			last = err
			continue
		}
		var reply Reply
		err = decode(response.Body, &reply, 8192)
		closeErr := response.Body.Close()
		if err != nil {
			return Reply{}, fmt.Errorf("invalid API response: %w", err)
		}
		if closeErr != nil {
			return Reply{}, closeErr
		}
		if response.StatusCode == http.StatusServiceUnavailable || (response.StatusCode == http.StatusTooManyRequests && reply.Status == "busy") {
			last = fmt.Errorf("endpoint unavailable: %s", reply.Error)
			continue
		}
		if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden || response.StatusCode == http.StatusBadRequest {
			return reply, fmt.Errorf("API rejected request: %s", reply.Error)
		}
		if response.StatusCode != replyCode(reply) {
			return Reply{}, fmt.Errorf("unexpected HTTP status %d", response.StatusCode)
		}
		return reply, nil
	}
	return Reply{}, fmt.Errorf("%w: %v", ErrUnavailable, last)
}

func randomRequest() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(value[:]), nil
}

func pause(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

type WorkerEvent struct {
	Status     string `json:"status"`
	Key        string `json:"key,omitempty"`
	Generation int    `json:"generation,omitempty"`
	Error      string `json:"error,omitempty"`
}

func Work(ctx context.Context, client *Client, leaseMS, maxJobs int, out io.Writer) error {
	if leaseMS < 1000 || leaseMS > 60000 || maxJobs < 0 || maxJobs > MaxJobs {
		return fmt.Errorf("lease must be 1000-60000 ms; max-jobs 0-4096")
	}
	encoder := json.NewEncoder(out)
	request, err := randomRequest()
	if err != nil {
		return err
	}
	completed := 0
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		reply, err := client.Call(ctx, "claim", Command{Request: request, LeaseMS: leaseMS})
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if !errors.Is(err, ErrUnavailable) {
				return err
			}
			if e := encoder.Encode(WorkerEvent{Status: "retrying", Error: err.Error()}); e != nil {
				return e
			}
			if err := pause(ctx, 500*time.Millisecond); err != nil {
				return err
			}
			continue
		}
		switch reply.Status {
		case "empty", "retired":
			request, err = randomRequest()
			if err != nil {
				return err
			}
			if err := pause(ctx, 200*time.Millisecond); err != nil {
				return err
			}
			continue
		case "busy":
			if err := pause(ctx, 200*time.Millisecond); err != nil {
				return err
			}
			continue
		case "claimed":
			if reply.Job == nil {
				return fmt.Errorf("claim missing job")
			}
		default:
			return fmt.Errorf("claim failed: %s: %s", reply.Status, reply.Error)
		}
		job := *reply.Job
		if err := encoder.Encode(WorkerEvent{Status: "claimed", Key: job.Key, Generation: job.Generation}); err != nil {
			return err
		}
		if err := pause(ctx, time.Duration(job.WorkMS)*time.Millisecond); err != nil {
			return err
		}
		for {
			reply, err = client.Call(ctx, "complete", Command{Key: job.Key, Generation: job.Generation, Result: Digest(job.Payload)})
			if err == nil {
				break
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if !errors.Is(err, ErrUnavailable) {
				return err
			}
			if e := encoder.Encode(WorkerEvent{Status: "retrying-completion", Key: job.Key, Generation: job.Generation, Error: err.Error()}); e != nil {
				return e
			}
			if err := pause(ctx, 500*time.Millisecond); err != nil {
				return err
			}
		}
		switch reply.Status {
		case "completed", "deduplicated":
			completed++
		case "stale":
		default:
			return fmt.Errorf("completion failed: %s: %s", reply.Status, reply.Error)
		}
		if err := encoder.Encode(WorkerEvent{Status: reply.Status, Key: job.Key, Generation: job.Generation}); err != nil {
			return err
		}
		if maxJobs > 0 && completed >= maxJobs {
			return nil
		}
		request, err = randomRequest()
		if err != nil {
			return err
		}
	}
}
