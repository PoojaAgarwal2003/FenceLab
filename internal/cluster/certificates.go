package cluster

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

func certificate(dir, name string, template, parent *x509.Certificate, parentKey *ecdsa.PrivateKey) (*x509.Certificate, *ecdsa.PrivateKey, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, err
	}
	template.SerialNumber = serial
	template.NotBefore = time.Now().Add(-5 * time.Minute)
	template.NotAfter = time.Now().Add(90 * 24 * time.Hour)
	if parent == nil {
		parent = template
		parentKey = key
	}
	der, err := x509.CreateCertificate(rand.Reader, template, parent, &key.PublicKey, parentKey)
	if err != nil {
		return nil, nil, err
	}
	private, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, nil, err
	}
	if err := os.WriteFile(filepath.Join(dir, name+".crt"), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600); err != nil {
		return nil, nil, err
	}
	if err := os.WriteFile(filepath.Join(dir, name+".key"), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: private}), 0600); err != nil {
		return nil, nil, err
	}
	parsed, err := x509.ParseCertificate(der)
	return parsed, key, err
}

// GeneratePKI creates local bootstrap credentials, not a public certificate
// service. The two issuers isolate consensus peers from API clients.
func GeneratePKI(directory string) error {
	if err := os.Mkdir(directory, 0700); err != nil {
		return fmt.Errorf("create new PKI directory: %w", err)
	}
	issuer := filepath.Join(directory, "issuer")
	if err := os.Mkdir(issuer, 0700); err != nil {
		return err
	}
	ca := func(name string) (*x509.Certificate, *ecdsa.PrivateKey, error) {
		return certificate(issuer, name, &x509.Certificate{Subject: pkix.Name{CommonName: name}, IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign}, nil, nil)
	}
	apiCA, apiKey, err := ca("api-ca")
	if err != nil {
		return err
	}
	raftCA, raftKey, err := ca("raft-ca")
	if err != nil {
		return err
	}
	copyCA := func(target, name string) error {
		data, err := os.ReadFile(filepath.Join(issuer, name+".crt"))
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(target, name+".crt"), data, 0600)
	}
	for i := 1; i <= 3; i++ {
		name := fmt.Sprintf("node%d", i)
		dir := filepath.Join(directory, name)
		if err := os.Mkdir(dir, 0700); err != nil {
			return err
		}
		for _, role := range []string{"api", "raft"} {
			parent, parentKey := apiCA, apiKey
			usage := []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
			if role == "raft" {
				parent, parentKey = raftCA, raftKey
				usage = append(usage, x509.ExtKeyUsageClientAuth)
			}
			template := &x509.Certificate{Subject: pkix.Name{CommonName: name + "-" + role}, DNSNames: []string{name, "localhost"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: usage}
			if _, _, err := certificate(dir, role, template, parent, parentKey); err != nil {
				return err
			}
			if err := copyCA(dir, role+"-ca"); err != nil {
				return err
			}
		}
	}
	for _, name := range []string{"admin", "worker1", "worker2", "probe"} {
		dir := filepath.Join(directory, name)
		if err := os.Mkdir(dir, 0700); err != nil {
			return err
		}
		role := "worker"
		if name == "admin" || name == "probe" {
			role = name
		}
		template := &x509.Certificate{Subject: pkix.Name{CommonName: name, OrganizationalUnit: []string{role}}, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}
		if _, _, err := certificate(dir, "client", template, apiCA, apiKey); err != nil {
			return err
		}
		if err := copyCA(dir, "api-ca"); err != nil {
			return err
		}
	}
	return nil
}

func LoadTLS(directory, role string, server bool) (*tls.Config, error) {
	if role != "raft" && role != "api" && role != "client" {
		return nil, fmt.Errorf("invalid credential role")
	}
	pair, err := tls.LoadX509KeyPair(filepath.Join(directory, role+".crt"), filepath.Join(directory, role+".key"))
	if err != nil {
		return nil, err
	}
	ca := "api-ca"
	if role == "raft" {
		ca = "raft-ca"
	}
	data, err := os.ReadFile(filepath.Join(directory, ca+".crt"))
	if err != nil {
		return nil, err
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(data) {
		return nil, fmt.Errorf("no trusted certificates")
	}
	config := &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{pair}, RootCAs: pool}
	if server {
		config.ClientCAs = pool
		config.ClientAuth = tls.RequireAndVerifyClientCert
	}
	return config, nil
}
