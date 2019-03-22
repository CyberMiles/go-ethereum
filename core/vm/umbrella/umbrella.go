package umbrella

import (
	"math/big"
	"github.com/ethereum/go-ethereum/common"
)

type Umbrella interface {
	GetValidators() []common.Address
	EmitScheduleTx(ScheduleTx)
	GetDueTxs() []ScheduleTx
	// schedule(this.A(a, b), timestamp);
	DefaultGasPrice() *big.Int
	FreeGasLimit() *big.Int
}
