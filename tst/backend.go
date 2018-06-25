// Copyright 2014 The go-ethereum Authors
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

// Package seke implements the sekechain protocol.
package seke

import (
	"errors"
	"fmt"
	"math/big"
	"runtime"
	"sync"
	"sync/atomic"

	"github.com/sekechain/go-sekechain/accounts"
	"github.com/sekechain/go-sekechain/common"
	"github.com/sekechain/go-sekechain/common/hexutil"
	"github.com/sekechain/go-sekechain/consensus"
	"github.com/sekechain/go-sekechain/consensus/clique"
	"github.com/sekechain/go-sekechain/consensus/ethash"
	"github.com/sekechain/go-sekechain/core"
	"github.com/sekechain/go-sekechain/core/bloombits"
	"github.com/sekechain/go-sekechain/core/types"
	"github.com/sekechain/go-sekechain/core/vm"
	"github.com/sekechain/go-sekechain/seke/downloader"
	"github.com/sekechain/go-sekechain/seke/filters"
	"github.com/sekechain/go-sekechain/seke/gasprice"
	"github.com/sekechain/go-sekechain/sekedb"
	"github.com/sekechain/go-sekechain/event"
	"github.com/sekechain/go-sekechain/internal/ethapi"
	"github.com/sekechain/go-sekechain/log"
	"github.com/sekechain/go-sekechain/miner"
	"github.com/sekechain/go-sekechain/node"
	"github.com/sekechain/go-sekechain/p2p"
	"github.com/sekechain/go-sekechain/params"
	"github.com/sekechain/go-sekechain/rlp"
	"github.com/sekechain/go-sekechain/rpc"
)

type LesServer interface {
	Start(srvr *p2p.Server)
	Stop()
	Protocols() []p2p.Protocol
	SetBloomBitsIndexer(bbIndexer *core.ChainIndexer)
}

// sekechain implements the sekechain full node service.
type sekechain struct {
	config      *Config
	chainConfig *params.ChainConfig

	// Channel for shutting down the service
	shutdownChan  chan bool    // Channel for shutting down the sekeereum
	stopDbUpgrade func() error // stop chain db sequential key upgrade

	// Handlers
	txPool          *core.TxPool
	blockchain      *core.BlockChain
	protocolManager *ProtocolManager
	lesServer       LesServer

	// DB interfaces
	chainDb sekedb.Database // Block chain database

	eventMux       *event.TypeMux
	engine         consensus.Engine
	accountManager *accounts.Manager

	bloomRequests chan chan *bloombits.Retrieval // Channel receiving bloom data retrieval requests
	bloomIndexer  *core.ChainIndexer             // Bloom indexer operating during block imports

	ApiBackend *sekeApiBackend

	miner     *miner.Miner
	gasPrice  *big.Int
	sekeerbase common.Address

	networkId     uint64
	netRPCService *ethapi.PublicNetAPI

	lock sync.RWMutex // Protects the variadic fields (e.g. gas price and sekeerbase)
}

func (s *sekechain) AddLesServer(ls LesServer) {
	s.lesServer = ls
	ls.SetBloomBitsIndexer(s.bloomIndexer)
}

// New creates a new sekechain object (including the
// initialisation of the common sekechain object)
func New(ctx *node.ServiceContext, config *Config) (*sekechain, error) {
	if config.SyncMode == downloader.LightSync {
		return nil, errors.New("can't run seke.sekechain in light sync mode, use les.Lightsekechain")
	}
	if !config.SyncMode.IsValid() {
		return nil, fmt.Errorf("invalid sync mode %d", config.SyncMode)
	}
	chainDb, err := CreateDB(ctx, config, "chaindata")
	if err != nil {
		return nil, err
	}
	stopDbUpgrade := upgradeDeduplicateData(chainDb)
	chainConfig, genesisHash, genesisErr := core.SetupGenesisBlock(chainDb, config.Genesis)
	if _, ok := genesisErr.(*params.ConfigCompatError); genesisErr != nil && !ok {
		return nil, genesisErr
	}
	log.Info("Initialised chain configuration", "config", chainConfig)

	seke := &sekechain{
		config:         config,
		chainDb:        chainDb,
		chainConfig:    chainConfig,
		eventMux:       ctx.EventMux,
		accountManager: ctx.AccountManager,
		engine:         CreateConsensusEngine(ctx, &config.sekeash, chainConfig, chainDb),
		shutdownChan:   make(chan bool),
		stopDbUpgrade:  stopDbUpgrade,
		networkId:      config.NetworkId,
		gasPrice:       config.GasPrice,
		sekeerbase:      config.sekeerbase,
		bloomRequests:  make(chan chan *bloombits.Retrieval),
		bloomIndexer:   NewBloomIndexer(chainDb, params.BloomBitsBlocks),
	}

	log.Info("Initialising sekechain protocol", "versions", ProtocolVersions, "network", config.NetworkId)

	if !config.SkipBcVersionCheck {
		bcVersion := core.GetBlockChainVersion(chainDb)
		if bcVersion != core.BlockChainVersion && bcVersion != 0 {
			return nil, fmt.Errorf("Blockchain DB version mismatch (%d / %d). Run gseke upgradedb.\n", bcVersion, core.BlockChainVersion)
		}
		core.WriteBlockChainVersion(chainDb, core.BlockChainVersion)
	}
	var (
		vmConfig    = vm.Config{EnablePreimageRecording: config.EnablePreimageRecording}
		cacheConfig = &core.CacheConfig{Disabled: config.NoPruning, TrieNodeLimit: config.TrieCache, TrieTimeLimit: config.TrieTimeout}
	)
	seke.blockchain, err = core.NewBlockChain(chainDb, cacheConfig, seke.chainConfig, seke.engine, vmConfig)
	if err != nil {
		return nil, err
	}
	// Rewind the chain in case of an incompatible config upgrade.
	if compat, ok := genesisErr.(*params.ConfigCompatError); ok {
		log.Warn("Rewinding chain to upgrade configuration", "err", compat)
		seke.blockchain.SetHead(compat.RewindTo)
		core.WriteChainConfig(chainDb, genesisHash, chainConfig)
	}
	seke.bloomIndexer.Start(seke.blockchain)

	if config.TxPool.Journal != "" {
		config.TxPool.Journal = ctx.ResolvePath(config.TxPool.Journal)
	}
	seke.txPool = core.NewTxPool(config.TxPool, seke.chainConfig, seke.blockchain)

	if seke.protocolManager, err = NewProtocolManager(seke.chainConfig, config.SyncMode, config.NetworkId, seke.eventMux, seke.txPool, seke.engine, seke.blockchain, chainDb); err != nil {
		return nil, err
	}
	seke.miner = miner.New(seke, seke.chainConfig, seke.EventMux(), seke.engine)
	seke.miner.SetExtra(makeExtraData(config.ExtraData))

	seke.ApiBackend = &sekeApiBackend{seke, nil}
	gpoParams := config.GPO
	if gpoParams.Default == nil {
		gpoParams.Default = config.GasPrice
	}
	seke.ApiBackend.gpo = gasprice.NewOracle(seke.ApiBackend, gpoParams)

	return seke, nil
}

func makeExtraData(extra []byte) []byte {
	if len(extra) == 0 {
		// create default extradata
		extra, _ = rlp.EncodeToBytes([]interface{}{
			uint(params.VersionMajor<<16 | params.VersionMinor<<8 | params.VersionPatch),
			"gseke",
			runtime.Version(),
			runtime.GOOS,
		})
	}
	if uint64(len(extra)) > params.MaximumExtraDataSize {
		log.Warn("Miner extra data exceed limit", "extra", hexutil.Bytes(extra), "limit", params.MaximumExtraDataSize)
		extra = nil
	}
	return extra
}

// CreateDB creates the chain database.
func CreateDB(ctx *node.ServiceContext, config *Config, name string) (sekedb.Database, error) {
	db, err := ctx.OpenDatabase(name, config.DatabaseCache, config.DatabaseHandles)
	if err != nil {
		return nil, err
	}
	if db, ok := db.(*sekedb.LDBDatabase); ok {
		db.Meter("seke/db/chaindata/")
	}
	return db, nil
}

// CreateConsensusEngine creates the required type of consensus engine instance for an sekechain service
func CreateConsensusEngine(ctx *node.ServiceContext, config *ethash.Config, chainConfig *params.ChainConfig, db sekedb.Database) consensus.Engine {
	// If proof-of-authority is requested, set it up
	if chainConfig.Clique != nil {
		return clique.New(chainConfig.Clique, db)
	}
	// Otherwise assume proof-of-work
	switch {
	case config.PowMode == ethash.ModeFake:
		log.Warn("sekeash used in fake mode")
		return ethash.NewFaker()
	case config.PowMode == ethash.ModeTest:
		log.Warn("sekeash used in test mode")
		return ethash.NewTester()
	case config.PowMode == ethash.ModeShared:
		log.Warn("sekeash used in shared mode")
		return ethash.NewShared()
	default:
		engine := ethash.New(ethash.Config{
			CacheDir:       ctx.ResolvePath(config.CacheDir),
			CachesInMem:    config.CachesInMem,
			CachesOnDisk:   config.CachesOnDisk,
			DatasetDir:     config.DatasetDir,
			DatasetsInMem:  config.DatasetsInMem,
			DatasetsOnDisk: config.DatasetsOnDisk,
		})
		engine.SetThreads(-1) // Disable CPU mining
		return engine
	}
}

// APIs returns the collection of RPC services the sekeereum package offers.
// NOTE, some of these services probably need to be moved to somewhere else.
func (s *sekechain) APIs() []rpc.API {
	apis := ethapi.GetAPIs(s.ApiBackend)

	// Append any APIs exposed explicitly by the consensus engine
	apis = append(apis, s.engine.APIs(s.BlockChain())...)

	// Append all the local APIs and return
	return append(apis, []rpc.API{
		{
			Namespace: "seke",
			Version:   "1.0",
			Service:   NewPublicsekechainAPI(s),
			Public:    true,
		}, {
			Namespace: "seke",
			Version:   "1.0",
			Service:   NewPublicMinerAPI(s),
			Public:    true,
		}, {
			Namespace: "seke",
			Version:   "1.0",
			Service:   downloader.NewPublicDownloaderAPI(s.protocolManager.downloader, s.eventMux),
			Public:    true,
		}, {
			Namespace: "miner",
			Version:   "1.0",
			Service:   NewPrivateMinerAPI(s),
			Public:    false,
		}, {
			Namespace: "seke",
			Version:   "1.0",
			Service:   filters.NewPublicFilterAPI(s.ApiBackend, false),
			Public:    true,
		}, {
			Namespace: "admin",
			Version:   "1.0",
			Service:   NewPrivateAdminAPI(s),
		}, {
			Namespace: "debug",
			Version:   "1.0",
			Service:   NewPublicDebugAPI(s),
			Public:    true,
		}, {
			Namespace: "debug",
			Version:   "1.0",
			Service:   NewPrivateDebugAPI(s.chainConfig, s),
		}, {
			Namespace: "net",
			Version:   "1.0",
			Service:   s.netRPCService,
			Public:    true,
		},
	}...)
}

func (s *sekechain) ResetWithGenesisBlock(gb *types.Block) {
	s.blockchain.ResetWithGenesisBlock(gb)
}

func (s *sekechain) sekeerbase() (eb common.Address, err error) {
	s.lock.RLock()
	sekeerbase := s.sekeerbase
	s.lock.RUnlock()

	if sekeerbase != (common.Address{}) {
		return sekeerbase, nil
	}
	if wallets := s.AccountManager().Wallets(); len(wallets) > 0 {
		if accounts := wallets[0].Accounts(); len(accounts) > 0 {
			sekeerbase := accounts[0].Address

			s.lock.Lock()
			s.sekeerbase = sekeerbase
			s.lock.Unlock()

			log.Info("sekeerbase automatically configured", "address", sekeerbase)
			return sekeerbase, nil
		}
	}
	return common.Address{}, fmt.Errorf("sekeerbase must be explicitly specified")
}

// set in js console via admin interface or wrapper from cli flags
func (self *sekechain) Setsekeerbase(sekeerbase common.Address) {
	self.lock.Lock()
	self.sekeerbase = sekeerbase
	self.lock.Unlock()

	self.miner.Setsekeerbase(sekeerbase)
}

func (s *sekechain) StartMining(local bool) error {
	eb, err := s.sekeerbase()
	if err != nil {
		log.Error("Cannot start mining without sekeerbase", "err", err)
		return fmt.Errorf("sekeerbase missing: %v", err)
	}
	if clique, ok := s.engine.(*clique.Clique); ok {
		wallet, err := s.accountManager.Find(accounts.Account{Address: eb})
		if wallet == nil || err != nil {
			log.Error("sekeerbase account unavailable locally", "err", err)
			return fmt.Errorf("signer missing: %v", err)
		}
		clique.Authorize(eb, wallet.SignHash)
	}
	if local {
		// If local (CPU) mining is started, we can disable the transaction rejection
		// mechanism introduced to speed sync times. CPU mining on mainnet is ludicrous
		// so noone will ever hit this path, whereas marking sync done on CPU mining
		// will ensure that private networks work in single miner mode too.
		atomic.StoreUint32(&s.protocolManager.acceptTxs, 1)
	}
	go s.miner.Start(eb)
	return nil
}

func (s *sekechain) StopMining()         { s.miner.Stop() }
func (s *sekechain) IsMining() bool      { return s.miner.Mining() }
func (s *sekechain) Miner() *miner.Miner { return s.miner }

func (s *sekechain) AccountManager() *accounts.Manager  { return s.accountManager }
func (s *sekechain) BlockChain() *core.BlockChain       { return s.blockchain }
func (s *sekechain) TxPool() *core.TxPool               { return s.txPool }
func (s *sekechain) EventMux() *event.TypeMux           { return s.eventMux }
func (s *sekechain) Engine() consensus.Engine           { return s.engine }
func (s *sekechain) ChainDb() sekedb.Database            { return s.chainDb }
func (s *sekechain) IsListening() bool                  { return true } // Always listening
func (s *sekechain) sekeVersion() int                    { return int(s.protocolManager.SubProtocols[0].Version) }
func (s *sekechain) NetVersion() uint64                 { return s.networkId }
func (s *sekechain) Downloader() *downloader.Downloader { return s.protocolManager.downloader }

// Protocols implements node.Service, returning all the currently configured
// network protocols to start.
func (s *sekechain) Protocols() []p2p.Protocol {
	if s.lesServer == nil {
		return s.protocolManager.SubProtocols
	}
	return append(s.protocolManager.SubProtocols, s.lesServer.Protocols()...)
}

// Start implements node.Service, starting all internal goroutines needed by the
// sekechain protocol implementation.
func (s *sekechain) Start(srvr *p2p.Server) error {
	// Start the bloom bits servicing goroutines
	s.startBloomHandlers()

	// Start the RPC service
	s.netRPCService = ethapi.NewPublicNetAPI(srvr, s.NetVersion())

	// Figure out a max peers count based on the server limits
	maxPeers := srvr.MaxPeers
	if s.config.LightServ > 0 {
		if s.config.LightPeers >= srvr.MaxPeers {
			return fmt.Errorf("invalid peer config: light peer count (%d) >= total peer count (%d)", s.config.LightPeers, srvr.MaxPeers)
		}
		maxPeers -= s.config.LightPeers
	}
	// Start the networking layer and the light server if requested
	s.protocolManager.Start(maxPeers)
	if s.lesServer != nil {
		s.lesServer.Start(srvr)
	}
	return nil
}

// Stop implements node.Service, terminating all internal goroutines used by the
// sekechain protocol.
func (s *sekechain) Stop() error {
	if s.stopDbUpgrade != nil {
		s.stopDbUpgrade()
	}
	s.bloomIndexer.Close()
	s.blockchain.Stop()
	s.protocolManager.Stop()
	if s.lesServer != nil {
		s.lesServer.Stop()
	}
	s.txPool.Stop()
	s.miner.Stop()
	s.eventMux.Stop()

	s.chainDb.Close()
	close(s.shutdownChan)

	return nil
}
