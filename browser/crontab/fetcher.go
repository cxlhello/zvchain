package crontab

import (
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

	trans := make([]*models.Transaction, 0)

	for _, tx := range b.Transactions {
		trans = append(trans, convertTransaction(types.NewTransaction(tx, tx.GenHash())))
	}

	evictedReceipts := make([]*models.Receipt, 0)

	receipts := make([]*models.Receipt, len(b.Transactions))
	for i, tx := range trans {
		wrapper := chain.GetTransactionPool().GetReceipt(common.HexToHash(tx.Hash))
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
		TxHash:            receipt.TxHash.String(),
		ContractAddress:   receipt.ContractAddress.String(),
	}
	return modelreceipt

}

func convertBlockHeader(b *types.Block) *models.Block {
	bh := b.Header
	block := &models.Block{
		Height:     bh.Height,
		Hash:       bh.Hash.String(),
		PreHash:    bh.PreHash.String(),
		CurTime:    bh.CurTime.Local(),
		PreTime:    bh.PreTime().Local(),
		Castor:     groupsig.DeserializeID(bh.Castor).String(),
		GroupID:    bh.Group.String(),
		TotalQN:    bh.TotalQN,
		TransCount: uint64(len(b.Transactions)),
		//Qn: mediator.Proc.CalcBlockHeaderQN(bh),

	}
	return block
}

func convertTransaction(tx *types.Transaction) *models.Transaction {
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
	trans := &models.Transaction{
		Hash:      tx.Hash.String(),
		Source:    tx.Source.String(),
		Target:    tx.Target.String(),
		Type:      int32(tx.Type),
		GasLimit:  gasLimit,
		GasPrice:  gasPrice,
		Data:      string(tx.Data),
		ExtraData: string(tx.ExtraData),
		Nonce:     tx.Nonce,
		Value:     common.RA2TAS(value),
	}
	return trans
}
