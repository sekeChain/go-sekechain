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

// Package les implements the Light sekechain Subprotocol.
package les

import (
	"fmt"
	"sync"
	"time"

	"github.com/sekechain/go-sekechain/accounts"
	"github.com/sekechain/go-sekechain/common"
	"github.com/sekechain/go-sekechain/common/hexutil"
	"github.com/sekechain/go-sekechain/consensus"
	"github.com/sekechain/go-sekechain/core"
	"github.com/sekechain/go-sekechain/core/bloombits"
	"github.com/sekechain/go-sekechain/core/types"
	"github.com/sekechain/go-sekechain/seke"
	"github.com/sekechain/go-sekechain/seke/downloader"
	"github.com/sekechain/go-sekechain/seke/filters"
	"github.com/sekechain/go-sekechain/seke/gasprice"
	"github.com/sekechain/go-sekechain/sekedb"
	"github.com/sekechain/go-sekechain/event"
	"github.com/sekechain/go-sekechain/internal/ethapi"
	"github.com/sekechain/go-sekechain/light"
	"github.com/sekechain/go-sekechain/log"
	"github.com/sekechain/go-sekechain/node"
	"github.com/sekechain/go-sekechain/p2p"
	"github.com/sekechain/go-sekechain/p2p/discv5"
	"github.com/sekechain/go-sekechain/params"
	rpc "github.com/sekechain/go-sekechain/rpc"
)

type Lightsekechain struct {
	config *seke.Config

	odr         *LesOdr
	relay       *LesTxRelay
	chainConfig *params.ChainConfig
	// Channel for shutting down the service
	shutdownChan chan bool
	// Handlers
	peers           *peerSet
	txPool          *light.TxPool
	blockchain      *light.LightChain
	protocolManager *ProtocolManager
	serverPool      *serverPool
	reqDist         *requestDistributor
	retriever       *retrieveManager
	// DB interfaces
	chainDb sekedb.Database // Block chain database

	bloomRequests                              chan chan *bloombits.Retrieval // Channel receiving bloom data retrieval requests
	bloomIndexer, chtIndexer, bloomTrieIndexer *core.ChainIndexer

	ApiBackend *LesApiBackend

	eventMux       *event.TypeMux
	engine         consensus.Engine
	accountManager *accounts.Manager

	networkId     uint64
	netRPCService *ethapi.PublicNetAPI

	wg sync.WaitGroup
}

func New(ctx *node.ServiceContext, config *seke.Config) (*Lightsekechain, error) {
	chainDb, err := seke.CreateDB(ctx, config, "lightchaindata")
	if err != nil {
		return nil, err
	}
	chainConfig, genesisHash, genesisErr := core.SetupGenesisBlock(chainDb, config.Genesis)
	if _, isCompat := genesisErr.(*params.ConfigCompatError); genesisErr != nil && !isCompat {
		return nil, genesisErr
	}
	log.Info("Initialised chain configuration", "config", chainConfig)

	peers := newPeerSet()
	quitSync := make(chan struct{})

	lseke := &Lightsekechain{
		config:           config,
		chainConfig:      chainConfig,
		chainDb:          chainDb,
		eventMux:         ctx.EventMux,
		peers:            peers,
		reqDist:          newRequestDistributor(peers, quitSync),
		accountManager:   ctx.AccountManager,
		engine:           seke.CreateConsensusEngine(ctx, &config.sekeash, chainConfig, chainDb),
		shutdownChan:     make(chan bool),
		networkId:        config.NetworkId,
		bloomRequests:    make(chan chan *bloombits.Retrieval),
		bloomIndexer:     seke.NewBloomIndexer(chainDb, light.BloomTrieFrequency),
		chtIndexer:       light.NewChtIndexer(chainDb, true),
		bloomTrieIndexer: light.NewBloomTrieIndexer(chainDb, true),
	}

	lseke.relay = NewLesTxRelay(peers, lseke.reqDist)
	lseke.serverPool = newServerPool(chainDb, quitSync, &lseke.wg)
	lseke.retriever = newRetrieveManager(peers, lseke.reqDist, lseke.serverPool)
	lseke.odr = NewLesOdr(chainDb, lseke.chtIndexer, lseke.bloomTrieIndexer, lseke.bloomIndexer, lseke.retriever)
	if lseke.blockchain, err = light.NewLightChain(lseke.odr, lseke.chainConfig, lseke.engine); err != nil {
		return nil, err
	}
	lseke.bloomIndexer.Start(lseke.blockchain)
	// Rewind the chain in case of an incompatible config upgrade.
	if compat, ok := genesisErr.(*params.ConfigCompatError); ok {
		log.Warn("Rewinding chain to upgrade configuration", "err", compat)
		lseke.blockchain.SetHead(compat.RewindTo)
		core.WriteChainConfig(chainDb, genesisHash, chainConfig)
	}

	lseke.txPool = light.NewTxPool(lseke.chainConfig, lseke.blockchain, lseke.relay)
	if lseke.protocolManager, err = NewProtocolManager(lseke.chainConfig, true, ClientProtocolVersions, config.NetworkId, lseke.eventMux, lseke.engine, lseke.peers, lseke.blockchain, nil, chainDb, lseke.odr, lseke.relay, quitSync, &lseke.wg); err != nil {
		return nil, err
	}
	lseke.ApiBackend = &LesApiBackend{lseke, nil}
	gpoParams := config.GPO
	if gpoParams.Default == nil {
		gpoParams.Default = config.GasPrice
	}
	lseke.ApiBackend.gpo = gasprice.NewOracle(lseke.ApiBackend, gpoParams)
	return lseke, nil
}

func lesTopic(genesisHash common.Hash, protocolVersion uint) discv5.Topic {
	var name string
	switch protocolVersion {
	case lpv1:
		name = "LES"
	case lpv2:
		name = "LES2"
	default:
		panic(nil)
	}
	return discv5.Topic(name + "@" + common.Bytes2Hex(genesisHash.Bytes()[0:8]))
}

type LightDummyAPI struct{}

// sekeerbase is the address that mining rewards will be send to
func (s *LightDummyAPI) sekeerbase() (common.Address, error) {
	return common.Address{}, fmt.Errorf("not supported")
}

// Coinbase is the address that mining rewards will be send to (alias for sekeerbase)
func (s *LightDummyAPI) Coinbase() (common.Address, error) {
	return common.Address{}, fmt.Errorf("not supported")
}

// Hashrate returns the POW hashrate
func (s *LightDummyAPI) Hashrate() hexutil.Uint {
	return 0
}

// Mining returns an indication if this node is currently mining.
func (s *LightDummyAPI) Mining() bool {
	return false
}

// APIs returns the collection of RPC services the sekeereum package offers.
// NOTE, some of these services probably need to be moved to somewhere else.
func (s *Lightsekechain) APIs() []rpc.API {
	return append(ethapi.GetAPIs(s.ApiBackend), []rpc.API{
		{
			Namespace: "seke",
			Version:   "1.0",
			Service:   &LightDummyAPI{},
			Public:    true,
		}, {
			Namespace: "seke",
			Version:   "1.0",
			Service:   downloader.NewPublicDownloaderAPI(s.protocolManager.downloader, s.eventMux),
			Public:    true,
		}, {
			Namespace: "seke",
			Version:   "1.0",
			Service:   filters.NewPublicFilterAPI(s.ApiBackend, true),
			Public:    true,
		}, {
			Namespace: "net",
			Version:   "1.0",
			Service:   s.netRPCService,
			Public:    true,
		},
	}...)
}

func (s *Lightsekechain) ResetWithGenesisBlock(gb *types.Block) {
	s.blockchain.ResetWithGenesisBlock(gb)
}

func (s *Lightsekechain) BlockChain() *light.LightChain      { return s.blockchain }
func (s *Lightsekechain) TxPool() *light.TxPool              { return s.txPool }
func (s *Lightsekechain) Engine() consensus.Engine           { return s.engine }
func (s *Lightsekechain) LesVersion() int                    { return int(s.protocolManager.SubProtocols[0].Version) }
func (s *Lightsekechain) Downloader() *downloader.Downloader { return s.protocolManager.downloader }
func (s *Lightsekechain) EventMux() *event.TypeMux           { return s.eventMux }

// Protocols implements node.Service, returning all the currently configured
// network protocols to start.
func (s *Lightsekechain) Protocols() []p2p.Protocol {
	return s.protocolManager.SubProtocols
}

// Start implements node.Service, starting all internal goroutines needed by the
// sekechain protocol implementation.
func (s *Lightsekechain) Start(srvr *p2p.Server) error {
	s.startBloomHandlers()
	log.Warn("Light client mode is an experimental feature")
	s.netRPCService = ethapi.NewPublicNetAPI(srvr, s.networkId)
	// clients are searching for the first advertised protocol in the list
	protocolVersion := AdvertiseProtocolVersions[0]
	s.serverPool.start(srvr, lesTopic(s.blockchain.Genesis().Hash(), protocolVersion))
	s.protocolManager.Start(s.config.LightPeers)
	return nil
}

// Stop implements node.Service, terminating all internal goroutines used by the
// sekechain protocol.
func (s *Lightsekechain) Stop() error {
	s.odr.Stop()
	if s.bloomIndexer != nil {
		s.bloomIndexer.Close()
	}
	if s.chtIndexer != nil {
		s.chtIndexer.Close()
	}
	if s.bloomTrieIndexer != nil {
		s.bloomTrieIndexer.Close()
	}
	s.blockchain.Stop()
	s.protocolManager.Stop()
	s.txPool.Stop()

	s.eventMux.Stop()

	time.Sleep(time.Millisecond * 200)
	s.chainDb.Close()
	close(s.shutdownChan)

	return nil
}
