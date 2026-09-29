package cluster

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"io"
	"log"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestAPIBoundaries(t *testing.T) {
	handler := Handler(nil, log.New(io.Discard, "", 0))
	for _, test := range []struct {
		path, role, method, body string
		want                     int
	}{
		{"/health/live", "", "GET", "", 401},
		{"/health/live", "unknown", "GET", "", 403},
		{"/health/live", "worker", "GET", "", 200},
		{"/v1/enqueue", "worker", "POST", "{}", 403},
		{"/v1/claim", "admin", "POST", "{}", 403},
		{"/v1/status", "worker", "GET", "", 403},
		{"/v1/status?key=a&key=b", "admin", "GET", "", 400},
		{"/v1/status?key=%xx", "admin", "GET", "", 400},
		{"/v1/enqueue", "admin", "GET", "{}", 400},
		{"/v1/enqueue", "admin", "POST", `{"unknown":1}`, 400},
		{"/v1/enqueue", "admin", "POST", `{"operation":"enqueue","operation":"claim"}`, 400},
		{"/v1/claim", "worker", "POST", `{"worker":"someone-else"}`, 403},
		{"/v1/claim", "worker", "POST", `{"operation":"enqueue"}`, 400},
		{"/missing", "admin", "GET", "", 404},
	} {
		t.Run(test.path+"-"+test.role+"-"+test.body, func(t *testing.T) {
			request := httptest.NewRequest(test.method, "https://localhost"+test.path, bytes.NewBufferString(test.body))
			request.Header.Set("Content-Type", "application/json")
			if test.role != "" {
				cert := &x509.Certificate{Subject: pkix.Name{CommonName: "worker1", OrganizationalUnit: []string{test.role}}}
				request.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{cert}, VerifiedChains: [][]*x509.Certificate{{cert}}}
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.want {
				t.Fatalf("%d: %s", response.Code, response.Body.String())
			}
		})
	}
}

func TestCredentialIsolationAndNoOverwrite(t *testing.T) {
	root := filepath.Join(t.TempDir(), "pki")
	if err := GeneratePKI(root); err != nil {
		t.Fatal(err)
	}
	if err := GeneratePKI(root); err == nil {
		t.Fatal("credentials overwritten")
	}
	worker, err := LoadTLS(filepath.Join(root, "worker1"), "client", false)
	if err != nil {
		t.Fatal(err)
	}
	peer, err := LoadTLS(filepath.Join(root, "node1"), "raft", true)
	if err != nil {
		t.Fatal(err)
	}
	clientCert, err := x509.ParseCertificate(worker.Certificates[0].Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := clientCert.Verify(x509.VerifyOptions{Roots: peer.ClientCAs, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}); err == nil {
		t.Fatal("API worker trusted as consensus peer")
	}
	for _, endpoint := range []string{"http://localhost", "https://user:secret@localhost", "https://localhost/path", "https://localhost?query=1"} {
		if _, err := NewClient([]string{endpoint}, worker); err == nil {
			t.Fatal("unsafe endpoint", endpoint)
		}
	}
	worker.InsecureSkipVerify = true
	if _, err := NewClient([]string{"https://localhost"}, worker); err == nil {
		t.Fatal("disabled certificate verification")
	}
}

func TestLateClaimReplayDoesNotConsumeAnotherJob(t *testing.T) {
	f := NewFSM()
	for _, key := range []string{"first", "second", "third"} {
		apply(t, f, Command{Operation: "enqueue", NowMS: 1000, Spec: Spec{Key: key, MaxAttempts: 2}})
	}
	for _, request := range []string{"request1", "request2"} {
		r := apply(t, f, Command{Operation: "claim", NowMS: 1000, Worker: "worker1", Request: request, LeaseMS: 1000})
		if r.Job == nil {
			t.Fatal(r)
		}
		apply(t, f, Command{Operation: "complete", NowMS: 1000, Worker: "worker1", Key: r.Job.Key, Generation: r.Job.Generation, Result: Digest(r.Job.Payload)})
	}
	r := apply(t, f, Command{Operation: "claim", NowMS: 1000, Worker: "worker1", Request: "request1", LeaseMS: 1000})
	if r.Status != "retired" || r.Counts.Pending != 1 || r.Counts.Leased != 0 {
		t.Fatal("late duplicate claimed new work", r)
	}
}
