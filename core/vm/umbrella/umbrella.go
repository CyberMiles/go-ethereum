package umbrella

import (
	"github.com/ethereum/go-ethereum/common"
)

type Umbrella interface {
	GetValidators() []common.Address
	EmitScheduleTx(ScheduleTx)
	GetDueTxs() []ScheduleTx
}
