package umbrella

import (
	"github.com/ethereum/go-ethereum/common"
)

type Umbrella struct {
	ValidatorManager     *ValidatorManager
	ScheduleTxManager    *ScheduleTxManager
	GasDiscountContracts map[common.Address]uint
}

func NewUmbrella() *Umbrella {
	umbrella := &Umbrella{
		ValidatorManager:     NewValidatorManager(),
		ScheduleTxManager:    NewScheduleTxManager(),
		GasDiscountContracts: make(map[common.Address]uint),
	}
	return umbrella
}
