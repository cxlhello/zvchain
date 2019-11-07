package tvm

import (
	"fmt"
)

var ContractTransferData chan *ContractTransfer
var TokenTransferData chan *TokenContractTransfer

type TokenContractTransfer struct {
	ContractAddr string
	Addr         []byte
	Value        []byte
}

type ContractTransfer struct {
	Value        uint64
	Address      string
	TxHash       string
	BlockHeight  uint64
	ContractCode string
}

func ProduceTokenContractTransfer(contratc string, addr []byte, value []byte) {
	contract := &TokenContractTransfer{
		ContractAddr: contratc,
		Addr:         addr,
		Value:        value,
	}
	TokenTransferData <- contract
	fmt.Println("ProduceTokenContractTransfer,addr:", string(addr), ",contractcode:", contratc, "value", string(value))
}
func ProduceContractTransfer(txHash string,
	addr string,
	value uint64,
	contractCode string,
	blockHeight uint64) {
	contract := &ContractTransfer{
		Value:        value,
		Address:      addr,
		TxHash:       txHash,
		ContractCode: contractCode,
		BlockHeight:  blockHeight,
	}
	ContractTransferData <- contract
	fmt.Println("ProduceContractTransfer,addr:", addr, ",contractcode:", contractCode)
}
