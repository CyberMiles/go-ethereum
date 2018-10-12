package eni

/*
#cgo CFLAGS: -Werror
#cgo LDFLAGS: -ldl -I${SRCDIR}/core/vm/eni
#include <dlfcn.h>
#include <stdint.h>
#include <stdlib.h>
#include <stdio.h>
#include "fork_call.h"

*/
import "C"

import (
	"debug/elf"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"unsafe"

	"github.com/ethereum/go-ethereum/node"
)

type ENI struct {
	// functions & dynamic libraries mapping used by ENI
	functions map[string]string
	opName    string
	gasFunc   unsafe.Pointer
	runFunc   unsafe.Pointer
	argsText  string // JSON
	retText   string // JSON
}

func NewENI() *ENI {
	return &ENI{}
}

func (eni *ENI) InitENI(eniFunction string, argsText string) (err error) {
	// Get functions & dynamic libraries mappings
	eni.functions, err = getEniFunctions()
	if err != nil {
		return err
	}

	dynamicLibName := eni.functions[eniFunction+"_gas"]
	// Load dynamic library.
	handler := C.dlopen(C.CString(dynamicLibName), C.RTLD_LAZY)
	if handler == nil {
		return errors.New("dlopen failed: " + dynamicLibName + "\nError: " + C.GoString(C.dlerror()))
	}

	eni.gasFunc = C.dlsym(handler, C.CString(eniFunction+"_gas"))
	if eni.gasFunc == nil {
		return errors.New("dlsym failed: " + eniFunction + "_gas")
	}
	eni.runFunc = C.dlsym(handler, C.CString(eniFunction+"_run"))
	if eni.runFunc == nil {
		return errors.New("dlsym failed: " + eniFunction + "_run")
	}
	eni.opName = eniFunction
	eni.argsText = argsText

	return nil
}

// Gas returns gas of current ENI operation
// a process is forked to achieve fault tolerance
func (eni *ENI) Gas() (uint64, error) {
	status := C.int(87)
	gas := uint64(C.fork_gas(eni.gasFunc, C.CString(eni.argsText), &status))
	if int(status) != 0 {
		return gas, errors.New("ENI " + eni.opName + " gas error" + ", status=" + fmt.Sprintf("%d", int(status)))
	}
	return gas, nil
}

// ExecuteENI executes current ENI operation
// a process is forked to achieve fault tolerance
func (eni *ENI) ExecuteENI() (string, error) {
	// Run ENI function.
	status := C.int(87)
	retCString := C.fork_run(eni.runFunc, C.CString(eni.argsText), &status)
	defer C.free(unsafe.Pointer(retCString))
	retGoString := C.GoString(retCString)

	if int(status) != 0 {
		return retGoString, errors.New("ENI " + eni.opName + " run error" + ", status=" + fmt.Sprintf("%d", int(status)))
	}
	return retGoString, nil
}

func getEniFunctions() (map[string]string, error) {
	if runtime.GOOS != "linux" {
		return nil, errors.New("currently ENI is only supported on Linux")
	}

	// Get dynamic library path.
	libPath := filepath.Join(node.DefaultDataDir(), "eni", "lib")
	if val, ok := os.LookupEnv("ENI_LIBRARY_PATH"); ok {
		libPath = val
	}
	var dynamicLibs []string
	fileinfo, err := os.Stat(libPath)
	if err != nil {
		return nil, err
	}
	if !fileinfo.Mode().IsDir() {
		return nil, errors.New("can't find dynamic library path: " + libPath)
	}

	// Get all dynamic libraries from library path.
	filepath.Walk(libPath, func(path string, fileinfo os.FileInfo, err error) error {
		if fileinfo.Mode().IsRegular() && filepath.Ext(path) == ".so" {
			dynamicLibs = append(dynamicLibs, path)
		}
		return nil
	})

	// Build mapping for functions and libraries
	functions := make(map[string]string)
	for _, lib := range dynamicLibs {
		elf, err := elf.Open(lib)
		if err != nil {
			return nil, err
		}

		symbols, err := elf.DynamicSymbols()
		if err != nil {
			return nil, err
		}

		for _, symbol := range symbols {
			functions[symbol.Name] = lib
		}
	}

	return functions, nil
}
