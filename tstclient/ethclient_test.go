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

package sekeclient

import "github.com/sekechain/go-sekechain"

// Verify that Client implements the sekeereum interfaces.
var (
	_ = sekeereum.ChainReader(&Client{})
	_ = sekeereum.TransactionReader(&Client{})
	_ = sekeereum.ChainStateReader(&Client{})
	_ = sekeereum.ChainSyncReader(&Client{})
	_ = sekeereum.ContractCaller(&Client{})
	_ = sekeereum.GasEstimator(&Client{})
	_ = sekeereum.GasPricer(&Client{})
	_ = sekeereum.LogFilterer(&Client{})
	_ = sekeereum.PendingStateReader(&Client{})
	// _ = sekeereum.PendingStateEventer(&Client{})
	_ = sekeereum.PendingContractCaller(&Client{})
)
