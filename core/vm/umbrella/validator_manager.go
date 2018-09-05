package umbrella

import (
	"github.com/ethereum/go-ethereum/common"
)

type Validator struct {
	Address common.Address // address of a validator
}

type ValidatorManager struct {
	validators map[Validator]bool // set to store validator list
}

func (vm *ValidatorManager) AppendValidator(v Validator) {
	vm.validators[v] = true
}

func (vm *ValidatorManager) RemoveValidator(v Validator) {
	delete(vm.validators, v)
}

func (vm *ValidatorManager) GetValidators() map[Validator]bool {
	return vm.validators
}

// NewValidatorManager returns a new ValidatorManager. The returned ValidatorManager will contain an empty list of validators.
func NewValidatorManager() *ValidatorManager {
	vm := &ValidatorManager{
		validators: make(map[Validator]bool),
	}
	return vm
}
