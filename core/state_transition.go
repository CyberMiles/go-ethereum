// Copyright 2014 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package core

import (
	"errors"
	"math"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"
)

var (
	errInsufficientBalanceForGas              = errors.New("insufficient balance to pay for gas")
	errInsufficientContractBalanceForFreeGas  = errors.New("insufficient contract balance to pay for gas")
	errCallNonFreeGasFunctionWithZeroGasPrice = errors.New("zero gasPrice transaction cannot call the non-freegas function")
	errCannotBuyGasFromNilAddress             = errors.New("cannot buy gas from nil address")
)

/*
The State Transitioning Model

A state transition is a change made when a transaction is applied to the current world state
The state transitioning model does all all the necessary work to work out a valid new state root.

1) Nonce handling
2) Pre pay gas
3) Create a new state object if the recipient is \0*32
4) Value transfer
== If contract creation ==
  4a) Attempt to run transaction data
  4b) If valid, use result as code for the new state object
== end ==
5) Run Script section
6) Derive new state root
*/
type StateTransition struct {
	gp         *GasPool
	msg        Message
	gas        uint64
	gasPrice   *big.Int
	initialGas uint64
	value      *big.Int
	data       []byte
	state      vm.StateDB
	evm        *vm.EVM
}

// Message represents a message sent to a contract.
type Message interface {
	From() common.Address
	//FromFrontier() (common.Address, error)
	To() *common.Address
	SetTo(*common.Address)
	SetData([]byte)

	GasPrice() *big.Int
	Gas() uint64
	Value() *big.Int

	Nonce() uint64
	CheckNonce() bool
	Data() []byte
}

// IntrinsicGas computes the 'intrinsic gas' for a message with the given data.
func IntrinsicGas(data []byte, contractCreation, homestead bool) (uint64, error) {
	// Set the starting gas for the raw transaction
	var gas uint64
	if contractCreation && homestead {
		gas = params.TxGasContractCreation
	} else {
		gas = params.TxGas
	}
	// Bump the required gas by the amount of transactional data
	if len(data) > 0 {
		// Zero and non-zero bytes are priced differently
		var nz uint64
		for _, byt := range data {
			if byt != 0 {
				nz++
			}
		}
		// Make sure we don't exceed uint64 for all data combinations
		if (math.MaxUint64-gas)/params.TxDataNonZeroGas < nz {
			return 0, vm.ErrOutOfGas
		}
		gas += nz * params.TxDataNonZeroGas

		z := uint64(len(data)) - nz
		if (math.MaxUint64-gas)/params.TxDataZeroGas < z {
			return 0, vm.ErrOutOfGas
		}
		gas += z * params.TxDataZeroGas
	}
	return gas, nil
}

// NewStateTransition initialises and returns a new state transition object.
func NewStateTransition(evm *vm.EVM, msg Message, gp *GasPool) *StateTransition {
	return &StateTransition{
		gp:       gp,
		evm:      evm,
		msg:      msg,
		gasPrice: msg.GasPrice(),
		value:    msg.Value(),
		data:     msg.Data(),
		state:    evm.StateDB,
	}
}

// ApplyMessage computes the new state by applying the given message
// against the old state within the environment.
//
// ApplyMessage returns the bytes returned by any EVM execution (if it took place),
// the gas used (which includes gas refunds) and an error if it failed. An error always
// indicates a core error meaning that the message would always fail for that particular
// state and would never be accepted within a block.
func ApplyMessage(evm *vm.EVM, msg Message, gp *GasPool) ([]byte, uint64, bool, error) {
	return NewStateTransition(evm, msg, gp).TransitionDb()
}

// to returns the recipient of the message.
func (st *StateTransition) to() common.Address {
	if st.msg == nil || st.msg.To() == nil /* contract creation */ {
		return common.Address{}
	}
	return *st.msg.To()
}

func (st *StateTransition) useGas(amount uint64) error {
	if st.gas < amount {
		return vm.ErrOutOfGas
	}
	st.gas -= amount

	return nil
}

func (st *StateTransition) buyGasFromSender() error {
	mgval := new(big.Int).Mul(new(big.Int).SetUint64(st.msg.Gas()), st.gasPrice)
	if st.state.GetBalance(st.msg.From()).Cmp(mgval) < 0 {
		return errInsufficientBalanceForGas
	}
	if err := st.gp.SubGas(st.msg.Gas()); err != nil {
		return err
	}
	st.gas += st.msg.Gas()

	st.initialGas = st.msg.Gas()
	st.state.SubBalance(st.msg.From(), mgval)
	return nil
}

func (st *StateTransition) buyGasFromContract() error {
	defaultGasPrice := st.evm.Context.Umbrella.DefaultGasPrice()
	mgval := new(big.Int).Mul(new(big.Int).SetUint64(st.msg.Gas()), defaultGasPrice)
	if st.msg.To() == nil {
		return errCannotBuyGasFromNilAddress
	}
	if st.state.GetBalance(*st.msg.To()).Cmp(mgval) < 0 {
		return errInsufficientContractBalanceForFreeGas
	}
	if err := st.gp.SubGas(st.msg.Gas()); err != nil {
		return err
	}
	st.gas += st.msg.Gas()

	st.initialGas = st.msg.Gas()

	st.state.SubBalance(*st.msg.To(), mgval)
	return nil
}

func (st *StateTransition) revertBuyGasFromContract() {
	defaultGasPrice := st.evm.Context.Umbrella.DefaultGasPrice()
	mgval := new(big.Int).Mul(new(big.Int).SetUint64(st.msg.Gas()), defaultGasPrice)
	st.gp.AddGas(st.gas)

	st.state.AddBalance(*st.msg.To(), mgval)
}

func (st *StateTransition) checkNonce() error {
	// Make sure this transaction's nonce is correct.
	if st.msg.CheckNonce() {
		nonce := st.state.GetNonce(st.msg.From())
		if nonce < st.msg.Nonce() {
			return ErrNonceTooHigh
		} else if nonce > st.msg.Nonce() {
			return ErrNonceTooLow
		}
	}
	return nil
}

// TransitionDb will transition the state by applying the current message and
// returning the result including the the used gas. It returns an error if it
// failed. An error indicates a consensus issue.
func (st *StateTransition) TransitionDb() (ret []byte, usedGas uint64, failed bool, err error) {
	// Check the nonce of this transaction is correct.
	if err = st.checkNonce(); err != nil {
		log.Debug("check nonce error", "err", err)
		return
	}

	// Get default gas limit and price from chain.
	var (
		defaultGasLimit = st.evm.Context.Umbrella.FreeGasLimit().Uint64()
		currentGasLimit = st.msg.Gas()
		currentGasPrice = st.gasPrice
		isFreeGasTX     = false
	)

	msg := st.msg
	sender := vm.AccountRef(msg.From())
	homestead := st.evm.ChainConfig().IsHomestead(st.evm.BlockNumber)
	contractCreation := msg.To() == nil
	contractCall := msg.To() != nil
	zeroGasPrice := currentGasPrice.Cmp(big.NewInt(0)) == 0

	if zeroGasPrice && currentGasLimit > defaultGasLimit && contractCall {
		// FreeGas TX
		isFreeGasTX = true
		log.Debug("trying to call a freegas function", "err", nil)

		// Check if the sender and contract have enough balance.
		if err = st.buyGasFromContract(); err != nil {
			log.Debug("insufficient contract balance", "err", err)
			return
		}
	} else {
		// Normal TX and Free TX
		// Check if the sender and contract have enough balance.
		if err = st.buyGasFromSender(); err != nil {
			log.Debug("insufficient sender balance", "err", err)
			return
		}
	}

	// Pay intrinsic gas
	gas, err := IntrinsicGas(st.data, contractCreation, homestead)
	if err != nil {
		log.Debug("insufficient gas to pay the intrinsic gas", "err", err)
		return nil, 0, false, err
	}
	if err = st.useGas(gas); err != nil {
		log.Debug("insufficient gas to pay the intrinsic gas", "err", err)
		return nil, 0, false, err
	}

	var (
		evm = st.evm
		// vm errors do not effect consensus and are therefor
		// not assigned to err, except for insufficient balance
		// error.
		vmerr error
	)

	evm.SetFreeGas(false)

	if contractCreation {
		ret, _, st.gas, vmerr = evm.Create(sender, st.data, st.gas, st.value)
	} else {
		// Increment the nonce for the next transaction
		st.state.SetNonce(msg.From(), st.state.GetNonce(sender.Address())+1)
		ret, st.gas, vmerr = evm.Call(sender, st.to(), st.data, st.gas, st.value)
	}
	if vmerr != nil {
		log.Debug("VM returned with error", "err", vmerr)
		// The only possible consensus-error would be if there wasn't
		// sufficient balance to make the transfer happen. The first
		// balance transfer may never fail.
		if vmerr == vm.ErrInsufficientBalance {
			return nil, 0, false, vmerr
		}
	}

	st.applyRefundGasCounter()

	if isFreeGasTX {
		if evm.IsFreeGas() {
			log.Debug("trigger freegas function, refund remaining gas to contract", "err", nil)
			st.refundGasToContract()
		} else {
			log.Debug("freegas transaction call the non-freegas function", "err", errCallNonFreeGasFunctionWithZeroGasPrice)
			st.revertBuyGasFromContract()
			return nil, 0, true, nil
		}
	} else {
		log.Debug("trigger normal function, refund remaining gas to sender", "err", nil)
		st.refundGasToSender()
	}

	// fee for miner
	st.state.AddBalance(st.evm.Coinbase, new(big.Int).Mul(new(big.Int).SetUint64(st.gasUsed()), st.gasPrice))
	return ret, st.gasUsed(), vmerr != nil, err
}

func (st *StateTransition) applyRefundGasCounter() {
	// Apply refund counter, capped to half of the used gas.
	refund := st.gasUsed() / 2
	if refund > st.state.GetRefund() {
		refund = st.state.GetRefund()
	}
	st.gas += refund
}

func (st *StateTransition) refundGasToSender() {
	// Return ETH for remaining gas, exchanged at the original rate.
	remaining := new(big.Int).Mul(new(big.Int).SetUint64(st.gas), st.gasPrice)
	st.state.AddBalance(st.msg.From(), remaining)

	// Also return remaining gas to the block gas counter so it is
	// available for the next transaction.
	st.gp.AddGas(st.gas)
}

func (st *StateTransition) refundGasToContract() {
	defaultGasPrice := st.evm.Context.Umbrella.DefaultGasPrice()
	// Return ETH for remaining gas, exchanged at the original rate.
	remaining := new(big.Int).Mul(new(big.Int).SetUint64(st.gas), defaultGasPrice)
	st.state.AddBalance(*st.msg.To(), remaining)
	// Also return remaining gas to the block gas counter so it is
	// available for the next transaction.
	st.gp.AddGas(st.gas)
}

// gasUsed returns the amount of gas used up by the state transition.
func (st *StateTransition) gasUsed() uint64 {
	return st.initialGas - st.gas
}
