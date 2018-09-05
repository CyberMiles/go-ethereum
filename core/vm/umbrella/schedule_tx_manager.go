package umbrella

import (
	"github.com/ethereum/go-ethereum/common"
)

type ScheduleTx struct {
	Sender   common.Address
	Receiver common.Address
	TxData   []byte
}

type ScheduleTxManager struct {
	scheduleToEmitTxs []ScheduleTx // EVM will execute all txs in this list.
	pendingSchduleTxs []ScheduleTx // EVM will store scheculed txs which are generate by `schedule()` function.
}

func (stm *ScheduleTxManager) AppendEmitTx(tx ScheduleTx) {
	stm.scheduleToEmitTxs = append(stm.scheduleToEmitTxs, tx)
}

func (stm *ScheduleTxManager) AppendPendingTx(tx ScheduleTx) {
	stm.pendingSchduleTxs = append(stm.pendingSchduleTxs, tx)
}

func (stm *ScheduleTxManager) GetScheduleToEmitTxs() []ScheduleTx {
	return stm.scheduleToEmitTxs
}

func (stm *ScheduleTxManager) GetPendingSchduleTxs() []ScheduleTx {
	return stm.pendingSchduleTxs
}

func NewScheduleTxManager() *ScheduleTxManager {
	stm := &ScheduleTxManager{}
	return stm
}
