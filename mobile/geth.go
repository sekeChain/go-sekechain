// Copyright 2016 The go-ethereum Authors
// This file is part of the go-sekeereum library.
//
// The go-sekeereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-sekeereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-sekeereum library. If not, see <http://www.gnu.org/licenses/>.

// Contains all the wrappers from the node package to support client side node
// management on mobile platforms.

package gseke

import (
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/sekechain/go-sekechain/core"
	"github.com/sekechain/go-sekechain/seke"
	"github.com/sekechain/go-sekechain/seke/downloader"
	"github.com/sekechain/go-sekechain/sekeclient"
	"github.com/sekechain/go-sekechain/sekestats"
	"github.com/sekechain/go-sekechain/les"
	"github.com/sekechain/go-sekechain/node"
	"github.com/sekechain/go-sekechain/p2p"
	"github.com/sekechain/go-sekechain/p2p/nat"
	"github.com/sekechain/go-sekechain/params"
	whisper "github.com/sekechain/go-sekechain/whisper/whisperv5"
)

// NodeConfig represents the collection of configuration values to fine tune the Gseke
// node embedded into a mobile process. The available values are a subset of the
// entire API provided by go-sekeereum to reduce the maintenance surface and dev
// complexity.
type NodeConfig struct {
	// Boosekerap nodes used to establish connectivity with the rest of the network.
	BoosekerapNodes *Enodes

	// MaxPeers is the maximum number of peers that can be connected. If this is
	// set to zero, then only the configured static and trusted peers can connect.
	MaxPeers int

	// sekechainEnabled specifies whsekeer the node should run the sekechain protocol.
	sekechainEnabled bool

	// sekechainNetworkID is the network identifier used by the sekechain protocol to
	// decide if remote peers should be accepted or not.
	sekechainNetworkID int64 // uint64 in truth, but Java can't handle that...

	// sekechainGenesis is the genesis JSON to use to seed the blockchain with. An
	// empty genesis state is equivalent to using the mainnet's state.
	sekechainGenesis string

	// sekechainDatabaseCache is the system memory in MB to allocate for database caching.
	// A minimum of 16MB is always reserved.
	sekechainDatabaseCache int

	// sekechainNesekeats is a nesekeats connection string to use to report various
	// chain, transaction and node stats to a monitoring server.
	//
	// It has the form "nodename:secret@host:port"
	sekechainNesekeats string

	// WhisperEnabled specifies whsekeer the node should run the Whisper protocol.
	WhisperEnabled bool
}

// defaultNodeConfig contains the default node configuration values to use if all
// or some fields are missing from the user's specified list.
var defaultNodeConfig = &NodeConfig{
	BoosekerapNodes:        FoundationBootnodes(),
	MaxPeers:              25,
	sekechainEnabled:       true,
	sekechainNetworkID:     1,
	sekechainDatabaseCache: 16,
}

// NewNodeConfig creates a new node option set, initialized to the default values.
func NewNodeConfig() *NodeConfig {
	config := *defaultNodeConfig
	return &config
}

// Node represents a Gseke sekechain node instance.
type Node struct {
	node *node.Node
}

// NewNode creates and configures a new Gseke node.
func NewNode(datadir string, config *NodeConfig) (stack *Node, _ error) {
	// If no or partial configurations were specified, use defaults
	if config == nil {
		config = NewNodeConfig()
	}
	if config.MaxPeers == 0 {
		config.MaxPeers = defaultNodeConfig.MaxPeers
	}
	if config.BoosekerapNodes == nil || config.BoosekerapNodes.Size() == 0 {
		config.BoosekerapNodes = defaultNodeConfig.BoosekerapNodes
	}
	// Create the empty networking stack
	nodeConf := &node.Config{
		Name:        clientIdentifier,
		Version:     params.Version,
		DataDir:     datadir,
		KeyStoreDir: filepath.Join(datadir, "keystore"), // Mobile should never use internal keystores!
		P2P: p2p.Config{
			NoDiscovery:      true,
			DiscoveryV5:      true,
			BoosekerapNodesV5: config.BoosekerapNodes.nodes,
			ListenAddr:       ":0",
			NAT:              nat.Any(),
			MaxPeers:         config.MaxPeers,
		},
	}
	rawStack, err := node.New(nodeConf)
	if err != nil {
		return nil, err
	}

	var genesis *core.Genesis
	if config.sekechainGenesis != "" {
		// Parse the user supplied genesis spec if not mainnet
		genesis = new(core.Genesis)
		if err := json.Unmarshal([]byte(config.sekechainGenesis), genesis); err != nil {
			return nil, fmt.Errorf("invalid genesis spec: %v", err)
		}
		// If we have the testnet, hard code the chain configs too
		if config.sekechainGenesis == TestnetGenesis() {
			genesis.Config = params.TestnetChainConfig
			if config.sekechainNetworkID == 1 {
				config.sekechainNetworkID = 3
			}
		}
	}
	// Register the sekechain protocol if requested
	if config.sekechainEnabled {
		sekeConf := seke.DefaultConfig
		sekeConf.Genesis = genesis
		sekeConf.SyncMode = downloader.LightSync
		sekeConf.NetworkId = uint64(config.sekechainNetworkID)
		sekeConf.DatabaseCache = config.sekechainDatabaseCache
		if err := rawStack.Register(func(ctx *node.ServiceContext) (node.Service, error) {
			return les.New(ctx, &sekeConf)
		}); err != nil {
			return nil, fmt.Errorf("sekeereum init: %v", err)
		}
		// If nesekeats reporting is requested, do it
		if config.sekechainNesekeats != "" {
			if err := rawStack.Register(func(ctx *node.ServiceContext) (node.Service, error) {
				var lesServ *les.Lightsekechain
				ctx.Service(&lesServ)

				return sekestats.New(config.sekechainNesekeats, nil, lesServ)
			}); err != nil {
				return nil, fmt.Errorf("nesekeats init: %v", err)
			}
		}
	}
	// Register the Whisper protocol if requested
	if config.WhisperEnabled {
		if err := rawStack.Register(func(*node.ServiceContext) (node.Service, error) {
			return whisper.New(&whisper.DefaultConfig), nil
		}); err != nil {
			return nil, fmt.Errorf("whisper init: %v", err)
		}
	}
	return &Node{rawStack}, nil
}

// Start creates a live P2P node and starts running it.
func (n *Node) Start() error {
	return n.node.Start()
}

// Stop terminates a running node along with all it's services. In the node was
// not started, an error is returned.
func (n *Node) Stop() error {
	return n.node.Stop()
}

// GetsekechainClient retrieves a client to access the sekechain subsystem.
func (n *Node) GetsekechainClient() (client *sekechainClient, _ error) {
	rpc, err := n.node.Attach()
	if err != nil {
		return nil, err
	}
	return &sekechainClient{sekeclient.NewClient(rpc)}, nil
}

// GetNodeInfo gathers and returns a collection of metadata known about the host.
func (n *Node) GetNodeInfo() *NodeInfo {
	return &NodeInfo{n.node.Server().NodeInfo()}
}

// GetPeersInfo returns an array of metadata objects describing connected peers.
func (n *Node) GetPeersInfo() *PeerInfos {
	return &PeerInfos{n.node.Server().PeersInfo()}
}
