// Copyright 2015 The go-ethereum Authors
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

package seke

import (
	"context"
	"math/big"

	"github.com/sekechain/go-sekechain/accounts"
	"github.com/sekechain/go-sekechain/common"
	"github.com/sekechain/go-sekechain/common/math"
	"github.com/sekechain/go-sekechain/core"
	"github.com/sekechain/go-sekechain/core/bloombits"
	"github.com/sekechain/go-sekechain/core/state"
	"github.com/sekechain/go-sekechain/core/types"
	"github.com/sekechain/go-sekechain/core/vm"
	"github.com/sekechain/go-sekechain/seke/downloader"
	"github.com/sekechain/go-sekechain/seke/gasprice"
	"github.com/sekechain/go-sekechain/sekedb"
	"github.com/sekechain/go-sekechain/event"
	"github.com/sekechain/go-sekechain/params"
	"github.com/sekechain/go-sekechain/rpc"
)

// sekeApiBackend implements ethapi.Backend for full nodes
type sekeApiBackend struct {
	seke *sekechain
	gpo *gasprice.Oracle
}

func (b *sekeApiBackend) ChainConfig() *params.ChainConfig {
	return b.seke.chainConfig
}

func (b *sekeApiBackend) CurrentBlock() *types.Block {
	return b.seke.blockchain.CurrentBlock()
}

func (b *sekeApiBackend) SetHead(number uint64) {
	b.seke.protocolManager.downloader.Cancel()
	b.seke.blockchain.SetHead(number)
}

func (b *sekeApiBackend) HeaderByNumber(ctx context.Context, blockNr rpc.BlockNumber) (*types.Header, error) {
	// Pending block is only known by the miner
	if blockNr == rpc.PendingBlockNumber {
		block := b.seke.miner.PendingBlock()
		return block.Header(), nil
	}
	// Otherwise resolve and return the block
	if blockNr == rpc.LatestBlockNumber {
		return b.seke.blockchain.CurrentBlock().Header(), nil
	}
	return b.seke.blockchain.GetHeaderByNumber(uint64(blockNr)), nil
}

func (b *sekeApiBackend) BlockByNumber(ctx context.Context, blockNr rpc.BlockNumber) (*types.Block, error) {
	// Pending block is only known by the miner
	if blockNr == rpc.PendingBlockNumber {
		block := b.seke.miner.PendingBlock()
		return block, nil
	}
	// Otherwise resolve and return the block
	if blockNr == rpc.LatestBlockNumber {
		return b.seke.blockchain.CurrentBlock(), nil
	}
	return b.seke.blockchain.GetBlockByNumber(uint64(blockNr)), nil
}

func (b *sekeApiBackend) StateAndHeaderByNumber(ctx context.Context, blockNr rpc.BlockNumber) (*state.StateDB, *types.Header, error) {
	// Pending state is only known by the miner
	if blockNr == rpc.PendingBlockNumber {
		block, state := b.seke.miner.Pending()
		return state, block.Header(), nil
	}
	// Otherwise resolve the block number and return its state
	header, err := b.HeaderByNumber(ctx, blockNr)
	if header == nil || err != nil {
		return nil, nil, err
	}
	stateDb, err := b.seke.BlockChain().StateAt(header.Root)
	return stateDb, header, err
}

func (b *sekeApiBackend) GetBlock(ctx context.Context, blockHash common.Hash) (*types.Block, error) {
	return b.seke.blockchain.GetBlockByHash(blockHash), nil
}

func (b *sekeApiBackend) GetReceipts(ctx context.Context, blockHash common.Hash) (types.Receipts, error) {
	return core.GetBlockReceipts(b.seke.chainDb, blockHash, core.GetBlockNumber(b.seke.chainDb, blockHash)), nil
}

func (b *sekeApiBackend) GetLogs(ctx context.Context, blockHash common.Hash) ([][]*types.Log, error) {
	receipts := core.GetBlockReceipts(b.seke.chainDb, blockHash, core.GetBlockNumber(b.seke.chainDb, blockHash))
	if receipts == nil {
		return nil, nil
	}
	logs := make([][]*types.Log, len(receipts))
	for i, receipt := range receipts {
		logs[i] = receipt.Logs
	}
	return logs, nil
}

func (b *sekeApiBackend) GetTd(blockHash common.Hash) *big.Int {
	return b.seke.blockchain.GetTdByHash(blockHash)
}

func (b *sekeApiBackend) GetEVM(ctx context.Context, msg core.Message, state *state.StateDB, header *types.Header, vmCfg vm.Config) (*vm.EVM, func() error, error) {
	state.SetBalance(msg.From(), math.MaxBig256)
	vmError := func() error { return nil }

	context := core.NewEVMContext(msg, header, b.seke.BlockChain(), nil)
	return vm.NewEVM(context, state, b.seke.chainConfig, vmCfg), vmError, nil
}

func (b *sekeApiBackend) SubscribeRemovedLogsEvent(ch chan<- core.RemovedLogsEvent) event.Subscription {
	return b.seke.BlockChain().SubscribeRemovedLogsEvent(ch)
}

func (b *sekeApiBackend) SubscribeChainEvent(ch chan<- core.ChainEvent) event.Subscription {
	return b.seke.BlockChain().SubscribeChainEvent(ch)
}

func (b *sekeApiBackend) SubscribeChainHeadEvent(ch chan<- core.ChainHeadEvent) event.Subscription {
	return b.seke.BlockChain().SubscribeChainHeadEvent(ch)
}

func (b *sekeApiBackend) SubscribeChainSideEvent(ch chan<- core.ChainSideEvent) event.Subscription {
	return b.seke.BlockChain().SubscribeChainSideEvent(ch)
}

func (b *sekeApiBackend) SubscribeLogsEvent(ch chan<- []*types.Log) event.Subscription {
	return b.seke.BlockChain().SubscribeLogsEvent(ch)
}

func (b *sekeApiBackend) SendTx(ctx context.Context, signedTx *types.Transaction) error {
	return b.seke.txPool.AddLocal(signedTx)
}

func (b *sekeApiBackend) GetPoolTransactions() (types.Transactions, error) {
	pending, err := b.seke.txPool.Pending()
	if err != nil {
		return nil, err
	}
	var txs types.Transactions
	for _, batch := range pending {
		txs = append(txs, batch...)
	}
	return txs, nil
}

func (b *sekeApiBackend) GetPoolTransaction(hash common.Hash) *types.Transaction {
	return b.seke.txPool.Get(hash)
}

func (b *sekeApiBackend) GetPoolNonce(ctx context.Context, addr common.Address) (uint64, error) {
	return b.seke.txPool.State().GetNonce(addr), nil
}

func (b *sekeApiBackend) Stats() (pending int, queued int) {
	return b.seke.txPool.Stats()
}

func (b *sekeApiBackend) TxPoolContent() (map[common.Address]types.Transactions, map[common.Address]types.Transactions) {
	return b.seke.TxPool().Content()
}

func (b *sekeApiBackend) SubscribeTxPreEvent(ch chan<- core.TxPreEvent) event.Subscription {
	return b.seke.TxPool().SubscribeTxPreEvent(ch)
}

func (b *sekeApiBackend) Downloader() *downloader.Downloader {
	return b.seke.Downloader()
}

func (b *sekeApiBackend) ProtocolVersion() int {
	return b.seke.sekeVersion()
}

func (b *sekeApiBackend) SuggestPrice(ctx context.Context) (*big.Int, error) {
	return b.gpo.SuggestPrice(ctx)
}

func (b *sekeApiBackend) ChainDb() sekedb.Database {
	return b.seke.ChainDb()
}

func (b *sekeApiBackend) EventMux() *event.TypeMux {
	return b.seke.EventMux()
}

func (b *sekeApiBackend) AccountManager() *accounts.Manager {
	return b.seke.AccountManager()
}

func (b *sekeApiBackend) BloomStatus() (uint64, uint64) {
	sections, _, _ := b.seke.bloomIndexer.Sections()
	return params.BloomBitsBlocks, sections
}

func (b *sekeApiBackend) ServiceFilter(ctx context.Context, session *bloombits.MatcherSession) {
	for i := 0; i < bloomFilterThreads; i++ {
		go session.Multiplex(bloomRetrievalBatch, bloomRetrievalWait, b.seke.bloomRequests)
	}
}
