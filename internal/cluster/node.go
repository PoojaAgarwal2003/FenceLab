package cluster

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/hashicorp/raft"
	raftboltdb "github.com/hashicorp/raft-boltdb/v2"
	bbolt "go.etcd.io/bbolt"
)

type Peer struct {
	ID      string `json:"id"`
	Address string `json:"address"`
}

type Config struct {
	ID        string
	Directory string
	Listen    string
	Peers     []Peer
	Bootstrap bool
	TLS       *tls.Config
	Log       io.Writer
}

type Node struct {
	Raft      *raft.Raft
	FSM       *FSM
	store     *raftboltdb.BoltStore
	transport *raft.NetworkTransport
}

type advertised string

func (a advertised) Network() string { return "tcp" }
func (a advertised) String() string  { return string(a) }

type secureStream struct {
	net.Listener
	address string
	config  *tls.Config
}

func (s *secureStream) Addr() net.Addr { return advertised(s.address) }
func (s *secureStream) Dial(address raft.ServerAddress, timeout time.Duration) (net.Conn, error) {
	return tls.DialWithDialer(&net.Dialer{Timeout: timeout}, "tcp", string(address), s.config)
}

func Open(c Config) (_ *Node, err error) {
	if !identifier.MatchString(c.ID) || c.Directory == "" || c.TLS == nil ||
		c.TLS.ClientAuth != tls.RequireAndVerifyClientCert || c.TLS.MinVersion < tls.VersionTLS13 ||
		len(c.Peers) != 3 || c.Log == nil {
		return nil, fmt.Errorf("three voters, mTLS 1.3 and a data directory are required")
	}
	servers := []raft.Server{}
	ids := map[string]bool{}
	addresses := map[string]bool{}
	ownAddress := ""
	for _, peer := range c.Peers {
		host, port, e := net.SplitHostPort(peer.Address)
		if e != nil || host == "" || port == "" || !identifier.MatchString(peer.ID) || ids[peer.ID] || addresses[peer.Address] {
			return nil, fmt.Errorf("invalid or duplicate peer")
		}
		ids[peer.ID] = true
		addresses[peer.Address] = true
		if peer.ID == c.ID {
			ownAddress = peer.Address
		}
		servers = append(servers, raft.Server{ID: raft.ServerID(peer.ID), Address: raft.ServerAddress(peer.Address), Suffrage: raft.Voter})
	}
	if ownAddress == "" {
		return nil, fmt.Errorf("local node missing from peers")
	}
	if err := os.MkdirAll(c.Directory, 0700); err != nil {
		return nil, err
	}
	if err := checkIdentity(c); err != nil {
		return nil, err
	}
	store, err := raftboltdb.New(raftboltdb.Options{Path: filepath.Join(c.Directory, "raft.db"), BoltOptions: &bbolt.Options{Timeout: time.Second}})
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, store.Close())
		}
	}()
	snaps, err := raft.NewFileSnapshotStore(c.Directory, 2, c.Log)
	if err != nil {
		return nil, err
	}
	listener, err := net.Listen("tcp", c.Listen)
	if err != nil {
		return nil, err
	}
	stream := &secureStream{Listener: tls.NewListener(listener, c.TLS), address: ownAddress, config: c.TLS}
	transport := raft.NewNetworkTransport(stream, 3, time.Second, c.Log)
	defer func() {
		if err != nil {
			err = errors.Join(err, transport.Close())
		}
	}()
	config := raft.DefaultConfig()
	config.LocalID = raft.ServerID(c.ID)
	config.LogOutput = c.Log
	config.HeartbeatTimeout = time.Second
	config.ElectionTimeout = time.Second
	config.LeaderLeaseTimeout = 500 * time.Millisecond
	config.SnapshotInterval = 20 * time.Second
	config.SnapshotThreshold = 128
	config.TrailingLogs = 64
	existing, err := raft.HasExistingState(store, store, snaps)
	if err != nil {
		return nil, err
	}
	if c.Bootstrap && !existing {
		if err := raft.BootstrapCluster(config, store, store, snaps, transport, raft.Configuration{Servers: servers}); err != nil {
			return nil, err
		}
	}
	fsm := NewFSM()
	r, err := raft.NewRaft(config, fsm, store, store, snaps, transport)
	if err != nil {
		return nil, err
	}
	return &Node{Raft: r, FSM: fsm, store: store, transport: transport}, nil
}

func (n *Node) Close() error {
	return errors.Join(n.Raft.Shutdown().Error(), n.transport.Close(), n.store.Close())
}

func (n *Node) Apply(c Command) (Reply, error) {
	c.NowMS = time.Now().UnixMilli()
	data, err := json.Marshal(c)
	if err != nil {
		return Reply{}, err
	}
	future := n.Raft.Apply(data, 2*time.Second)
	if err := future.Error(); err != nil {
		return Reply{}, err
	}
	reply, ok := future.Response().(Reply)
	if !ok {
		return Reply{}, fmt.Errorf("unexpected committed response")
	}
	return reply, nil
}

func (n *Node) Query(key string) (Reply, error) {
	if err := n.Raft.Barrier(2 * time.Second).Error(); err != nil {
		return Reply{}, err
	}
	return n.FSM.View(key), nil
}

func checkIdentity(c Config) error {
	peers := slices.Clone(c.Peers)
	slices.SortFunc(peers, func(a, b Peer) int {
		if a.ID < b.ID {
			return -1
		}
		if a.ID > b.ID {
			return 1
		}
		return 0
	})
	identity := struct {
		Version string
		ID      string
		Peers   []Peer
	}{Version, c.ID, peers}
	expected, err := json.Marshal(identity)
	if err != nil {
		return err
	}
	path := filepath.Join(c.Directory, "identity.json")
	existing, err := os.ReadFile(path)
	if err == nil {
		if string(existing) != string(expected) {
			return fmt.Errorf("data directory belongs to a different node or topology")
		}
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(expected)
	if writeErr == nil {
		writeErr = file.Sync()
	}
	return errors.Join(writeErr, file.Close())
}
