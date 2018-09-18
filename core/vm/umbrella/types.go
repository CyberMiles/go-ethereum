package umbrella

import (
	"github.com/ethereum/go-ethereum/common"
)

type ScheduleTx struct {
	Sender   common.Address
	Receiver common.Address
	TxData   []byte
}

