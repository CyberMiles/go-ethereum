package eni

// type format (typeInfo) grammar
// typeInfo describe the types with encoding
// type: bool | int | uint | address | bytes | enum | string | fix_array | dyn_array | struct
// fix_array: fix_array_start [0-9]+ type
// dyn_array: dyn_array_start type
// struct: struct_start type+ struct_end

// TODO: bytes

import (
	"bytes"
	"errors"
	"fmt"
	"math/big"
)

type argConverter struct{}

type Memory interface {
	Get(int64, int64) []byte
}

// ConvertArguments converts arguments from ENI encoding to JSON
func ConvertArguments(typeInfo []byte, data []byte, memory Memory) (ret string, err error) {
	var cvt *argConverter
	defer func() {
		if r := recover(); r != nil {
			err = errors.New(fmt.Sprint("Argument Parser Error: ", r))
		}
	}()
	var json bytes.Buffer

	json.WriteString("[")
	for i := 0; 0 < len(typeInfo); i++ {
		if 0 < i {
			json.WriteString(",")
		}
		typeInfo, data = cvt.parseType(typeInfo, data, &json, memory)
	}
	json.WriteString("]")

	return json.String(), err
}

// assuming that data are packed
func (cvt *argConverter) parseType(typeInfo []byte, data []byte, json *bytes.Buffer, memory Memory) ([]byte, []byte) {
	t := typeInfo[0]
	if ComplexType[t] {
		if t == FIX_ARRAY_START {
			typeInfo, data = cvt.parseFixArray(typeInfo, data, json, memory)
		} else if t == DYN_ARRAY_START {
			typeInfo, data = cvt.parseDynArray(typeInfo, data, json, memory)
		} else if t == STRUCT_START {
			typeInfo, data = cvt.parseStruct(typeInfo, data, json, memory)
		} else if t == STRING {
			typeInfo, data = cvt.parseString(typeInfo, data, json, memory)
		} else if t == STRINGPTR {
			typeInfo, data = cvt.parseStringPtr(typeInfo, data, json, memory)
		}
	} else { // value type
		typeInfo, data = cvt.parseValue(typeInfo, data, json, memory)
	}
	return typeInfo, data
}

func (cvt *argConverter) parseString(typeInfo []byte, data []byte, json *bytes.Buffer, memory Memory) ([]byte, []byte) {
	typeInfo = typeInfo[1:] // string
	leng := new(big.Int).SetBytes(data[:32]).Int64()
	data = data[32:]

	var buffer bytes.Buffer
	for i := int64(0); i < leng; i++ {
		if data[i] == '\\' || data[i] == '"' {
			buffer.WriteByte('\\')
			buffer.WriteByte(data[i])
		} else if data[i] < 0x20 { // control characters
			buffer.WriteString(fmt.Sprintf("\\u%04X", data[i]))
		} else {
			buffer.WriteByte(data[i])
		}
	}
	json.WriteString("\"")
	json.WriteString(buffer.String())
	json.WriteString("\"")
	data = data[leng:]
	if leng%32 > 0 {
		data = data[32-leng%32:]
	}
	return typeInfo, data
}

func (cvt *argConverter) parseStringPtr(typeInfo []byte, data []byte, json *bytes.Buffer, memory Memory) ([]byte, []byte) {
	ptr := new(big.Int).SetBytes(data[:32]).Int64()
	len := new(big.Int).SetBytes(memory.Get(ptr, 32)).Int64()
	str := memory.Get(ptr+32, len)

	var buffer bytes.Buffer
	for i := int64(0); i < len; i++ {
		if str[i] == '\\' || str[i] == '"' {
			buffer.WriteByte('\\')
			buffer.WriteByte(str[i])
		} else if str[i] < 0x20 { // control characters
			buffer.WriteString(fmt.Sprintf("\\u%04X", str[i]))
		} else {
			buffer.WriteByte(str[i])
		}
	}
	json.WriteString("\"")
	json.WriteString(buffer.String())
	json.WriteString("\"")

	typeInfo = typeInfo[1:]
	data = data[32:]

	return typeInfo, data
}

// parsing int32 not finished
func (cvt *argConverter) parseFixArray(typeInfo []byte, data []byte, json *bytes.Buffer, memory Memory) ([]byte, []byte) {
	typeInfo = typeInfo[1:] // fix_array_start
	json.WriteString("[")
	leng := new(big.Int).SetBytes(typeInfo[:32]).Int64()
	typeInfo = typeInfo[32:]

	for i := int64(0); i < leng; i++ {
		if i == leng-1 {
			typeInfo, data = cvt.parseType(typeInfo, data, json, memory)
		} else {
			json.WriteString(", ")
			_, data = cvt.parseType(typeInfo, data, json, memory)
		}
	}

	json.WriteString("]")
	return typeInfo, data
}

// dynamic array
func (cvt *argConverter) parseDynArray(typeInfo []byte, data []byte, json *bytes.Buffer, memory Memory) ([]byte, []byte) {
	panic(fmt.Sprintf("dynamic array not implemented yet!"))
}

func (cvt *argConverter) parseStruct(typeInfo []byte, data []byte, json *bytes.Buffer, memory Memory) ([]byte, []byte) {
	typeInfo = typeInfo[1:] // struct_start
	json.WriteString("[")
	for i := 0; 0 < len(typeInfo); i++ {
		t := typeInfo[0]
		if 0 < i {
			json.WriteString(", ")
		}
		if t != STRUCT_END {
			typeInfo, data = cvt.parseType(typeInfo, data, json, memory)
		} else {
			break
		}
	}
	if typeInfo[0] != STRUCT_END {
		panic("encoding error - expected struct_end token")
	}
	typeInfo = typeInfo[1:] // struct_end
	json.WriteString("]")
	return typeInfo, data
}

// bool, int
func (cvt *argConverter) parseValue(typeInfo []byte, data []byte, json *bytes.Buffer, memory Memory) ([]byte, []byte) {
	t := typeInfo[0]
	if t == BOOL {
		var boolVal bool
		for i := 0; i < 32; i++ {
			if data[i] != 0 {
				boolVal = true
			}
		}
		json.WriteString(fmt.Sprint(boolVal))
	} else if IsSint(t) { // signed integer
		n := new(big.Int)
		var b [32]byte
		copy(b[:], data[:32])
		if b[0] >= 128 { // negative value, two's complement
			n.SetBytes(b[:])
			n = n.Sub(n, big.NewInt(int64(1)))
			copy(b[:], n.Bytes())
			for i := 0; i < 32; i++ {
				b[i] ^= 255
			}
			n.SetBytes(b[:])
			n = n.Mul(n, big.NewInt(int64(-1)))
			json.WriteString(n.String())
		} else { // positive value
			n.SetBytes(b[:])
			json.WriteString(n.String())
		}
	} else if IsUint(t) { // unsigned integer
		n := new(big.Int)
		n.SetBytes(data[:32]) // big endian
		json.WriteString(n.String())
	} else {
		panic(fmt.Sprintf("encoding error - unknown or not implemented type: %d", t))
	}
	typeInfo = typeInfo[1:]
	data = data[32:]
	return typeInfo, data
}
