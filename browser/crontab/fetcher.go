package crontab

import (
	"encoding/base64"
	"fmt"
	"github.com/zvchain/zvchain/browser/models"
	"github.com/zvchain/zvchain/browser/util"
	"github.com/zvchain/zvchain/common"
	"github.com/zvchain/zvchain/consensus/groupsig"
	"github.com/zvchain/zvchain/core"
	"github.com/zvchain/zvchain/middleware/types"
)

type Fetcher struct {
}

func (api *Fetcher) ExplorerBlockDetail(height uint64) (*models.BlockDetail, error) {
	chain := core.BlockChainImpl
	b := chain.QueryBlockCeil(height)
	if b == nil {
		return nil, fmt.Errorf("queryBlock error")
	}
	block := convertBlockHeader(b)

	trans := make([]*models.TempTransaction, 0)

	for _, tx := range b.Transactions {
		trans = append(trans, convertTransaction(types.NewTransaction(tx, tx.GenHash())))
	}

	evictedReceipts := make([]*models.Receipt, 0)

	receipts := make([]*models.Receipt, len(b.Transactions))
	for i, tx := range trans {
		wrapper := chain.GetTransactionPool().GetReceipt(tx.Hash)
		if wrapper != nil {
			modelreceipt := convertReceipt(wrapper)
			receipts[i] = modelreceipt
		}
	}

	bd := &models.BlockDetail{
		Block:           *block,
		Trans:           trans,
		EvictedReceipts: evictedReceipts,
		Receipts:        receipts,
	}
	fmt.Println("ExplorerBlockDetail", util.ObjectTojson(bd))
	return bd, nil
}

func convertReceipt(receipt *types.Receipt) *models.Receipt {
	modelreceipt := &models.Receipt{
		Status:            uint(receipt.Status),
		CumulativeGasUsed: receipt.CumulativeGasUsed,
		Logs:              nil,
		TxHash:            receipt.TxHash.Hex(),
		ContractAddress:   receipt.ContractAddress.AddrPrefixString(),
	}
	return modelreceipt

}

func convertBlockHeader(b *types.Block) *models.Block {
	bh := b.Header
	block := &models.Block{
		Height:     bh.Height,
		Hash:       bh.Hash.Hex(),
		PreHash:    bh.PreHash.Hex(),
		CurTime:    bh.CurTime.Local(),
		PreTime:    bh.PreTime().Local(),
		Castor:     groupsig.DeserializeID(bh.Castor).GetAddrString(),
		GroupID:    bh.Group.Hex(),
		TotalQN:    bh.TotalQN,
		TransCount: uint64(len(b.Transactions)),
		//Qn: mediator.Proc.CalcBlockHeaderQN(bh),

	}
	return block
}

func (api *Fetcher) ConvertTempTransactionToTransaction(temp *models.TempTransaction) *models.Transaction {

	tran := &models.Transaction{
		Data:      util.ObjectTojson(temp.Data),
		Value:     temp.Value,
		Nonce:     temp.Nonce,
		Type:      int32(temp.Type),
		GasLimit:  temp.GasLimit,
		GasPrice:  temp.GasPrice,
		Hash:      temp.Hash.Hex(),
		ExtraData: temp.ExtraData,
	}
	if temp.Source != nil {
		tran.Source = temp.Source.AddrPrefixString()
	}
	if temp.Target != nil {
		tran.Target = temp.Target.AddrPrefixString()
	}
	return tran

}

func convertTransaction(tx *types.Transaction) *models.TempTransaction {
	var (
		gasLimit = uint64(0)
		gasPrice = uint64(0)
		value    = uint64(0)
	)
	if tx.GasLimit != nil {
		gasLimit = tx.GasLimit.Uint64()
	}
	if tx.GasPrice != nil {
		gasPrice = tx.GasPrice.Uint64()
	}
	if tx.Value != nil {
		value = tx.Value.Uint64()
	}
	trans := &models.TempTransaction{
		Hash:      tx.Hash,
		Source:    tx.Source,
		Target:    tx.Target,
		Type:      tx.Type,
		GasLimit:  gasLimit,
		GasPrice:  gasPrice,
		Data:      tx.Data,
		ExtraData: base64.StdEncoding.EncodeToString(tx.ExtraData),
		Nonce:     tx.Nonce,
		Value:     common.RA2TAS(value),
	}
	return trans
}
