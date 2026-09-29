package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"strings"
	"time"

	"github.com/PoojaAgarwal2003/FenceLab/internal/cluster"
)

func clusterCommand(ctx context.Context, command string, args []string, out, errOut io.Writer) int {
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(errOut)
	directory, credentials, id, listen, api, peerList, endpoints, operation, file, key := "", "", "", "127.0.0.1:7001", "127.0.0.1:8101", "", "", "status", "", ""
	bootstrap := false
	leaseMS, maxJobs := 5000, 0
	switch command {
	case "cluster-pki":
		flags.StringVar(&directory, "dir", "", "new directory for separate API/Raft CAs and local bootstrap certificates")
	case "cluster-node":
		flags.StringVar(&directory, "dir", "", "persistent node-specific data directory")
		flags.StringVar(&credentials, "credentials", "", "directory containing api/raft certificates, keys and CAs")
		flags.StringVar(&id, "id", "", "fixed voter ID")
		flags.StringVar(&listen, "raft-listen", listen, "Raft bind address")
		flags.StringVar(&api, "api-listen", api, "HTTPS bind address")
		flags.StringVar(&peerList, "peers", "", "three id=host:port voters, comma separated")
		flags.BoolVar(&bootstrap, "bootstrap", false, "bootstrap only the first voter on empty storage")
	case "cluster-client", "cluster-worker":
		flags.StringVar(&credentials, "credentials", "", "admin or worker certificate directory")
		flags.StringVar(&endpoints, "endpoints", "", "comma-separated HTTPS API addresses")
		if command == "cluster-client" {
			flags.StringVar(&operation, "operation", "status", "enqueue, status, health or ready")
			flags.StringVar(&file, "file", "", "job spec JSON for enqueue")
			flags.StringVar(&key, "key", "", "job key for status")
		} else {
			flags.IntVar(&leaseMS, "lease-ms", 5000, "ownership lease duration (1000-60000 ms)")
			flags.IntVar(&maxJobs, "max-jobs", 0, "exit after acknowledged jobs; 0 runs until stopped")
		}
	default:
		fmt.Fprintln(errOut, "unknown cluster command")
		return 2
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(errOut, "no positional arguments accepted")
		return 2
	}
	fail := func(err error) int { fmt.Fprintln(errOut, err); return 1 }
	switch command {
	case "cluster-pki":
		if directory == "" {
			fmt.Fprintln(errOut, "-dir required")
			return 2
		}
		if err := cluster.GeneratePKI(directory); err != nil {
			return fail(err)
		}
		if _, err := fmt.Fprintln(out, "Created separate API/Raft issuers and node1-node3, admin, worker1-worker2 and probe credentials. Keep issuer keys offline; never commit credentials."); err != nil {
			return fail(err)
		}
		return 0
	case "cluster-node":
		if directory == "" || credentials == "" || id == "" || peerList == "" {
			fmt.Fprintln(errOut, "-dir, -credentials, -id and -peers required")
			return 2
		}
		var peers []cluster.Peer
		for _, value := range strings.Split(peerList, ",") {
			parts := strings.SplitN(value, "=", 2)
			if len(parts) != 2 {
				fmt.Fprintln(errOut, "invalid peer list")
				return 2
			}
			peers = append(peers, cluster.Peer{ID: parts[0], Address: parts[1]})
		}
		peerTLS, err := cluster.LoadTLS(credentials, "raft", true)
		if err != nil {
			return fail(err)
		}
		apiTLS, err := cluster.LoadTLS(credentials, "api", true)
		if err != nil {
			return fail(err)
		}
		node, err := cluster.Open(cluster.Config{ID: id, Directory: directory, Listen: listen, Peers: peers, Bootstrap: bootstrap, TLS: peerTLS, Log: errOut})
		if err != nil {
			return fail(err)
		}
		logger := log.New(errOut, "cluster-api: ", log.LstdFlags)
		server, done, err := cluster.StartHTTP(api, apiTLS, cluster.Handler(node, logger), logger)
		if err != nil {
			return fail(errors.Join(err, node.Close()))
		}
		select {
		case <-ctx.Done():
			shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			err = server.Shutdown(shutdown)
			cancel()
			if err != nil {
				err = errors.Join(err, server.Close())
			}
			err = errors.Join(err, <-done)
		case err = <-done:
		}
		err = errors.Join(err, node.Close())
		if err != nil {
			return fail(err)
		}
		return 0
	}
	if credentials == "" || endpoints == "" {
		fmt.Fprintln(errOut, "-credentials and -endpoints required")
		return 2
	}
	config, err := cluster.LoadTLS(credentials, "client", false)
	if err != nil {
		return fail(err)
	}
	client, err := cluster.NewClient(strings.Split(endpoints, ","), config)
	if err != nil {
		return fail(err)
	}
	defer client.Close()
	if command == "cluster-worker" {
		err := cluster.Work(ctx, client, leaseMS, maxJobs, out)
		if err != nil && !errors.Is(err, context.Canceled) {
			return fail(err)
		}
		return 0
	}
	request := cluster.Command{Key: key}
	switch operation {
	case "enqueue":
		if file == "" || key != "" {
			fmt.Fprintln(errOut, "enqueue requires -file without -key")
			return 2
		}
		if err := readModelFile(file, &request.Spec); err != nil {
			fmt.Fprintln(errOut, err)
			return 2
		}
	case "status", "health", "ready":
		if file != "" || (operation != "status" && key != "") {
			fmt.Fprintln(errOut, "unexpected file/key")
			return 2
		}
	default:
		fmt.Fprintln(errOut, "unsupported client operation")
		return 2
	}
	reply, err := client.Call(ctx, operation, request)
	if err != nil {
		return fail(err)
	}
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(reply); err != nil {
		return fail(err)
	}
	if reply.Error != "" {
		return 1
	}
	return 0
}
