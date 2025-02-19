package common

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	browserlog "github.com/zvchain/zvchain/browser/log"
	"github.com/zvchain/zvchain/browser/models"
	"github.com/zvchain/zvchain/browser/util"
	"github.com/zvchain/zvchain/common"
	"github.com/zvchain/zvchain/consensus/groupsig"
	"github.com/zvchain/zvchain/core"
	"github.com/zvchain/zvchain/middleware/types"
	"github.com/zvchain/zvchain/tvm"
	"strings"
)

type Fetcher struct {
}
type MortGage struct {
	Stake                uint64             `json:"stake"`
	ApplyHeight          uint64             `json:"apply_height"`
	Type                 string             `json:"type"`
	Status               types.MinerStatus  `json:"miner_status"`
	StatusUpdateHeight   uint64             `json:"status_update_height"`
	Identity             types.NodeIdentity `json:"identity"`
	IdentityUpdateHeight uint64             `json:"identity_update_height"`
}

type LogData struct {
	User  string
	Args  []string
	Group string
	Value uint64
}

type Group struct {
	Seed          common.Hash `json:"id"`
	BeginHeight   uint64      `json:"begin_height"`
	DismissHeight uint64      `json:"dismiss_height"`
	Threshold     int32       `json:"threshold"`
	Members       []string    `json:"members"`
	MemSize       int         `json:"mem_size"`
	GroupHeight   uint64      `json:"group_height"`
}

type ContractCall struct {
	Hash string
}
type FronzenAndStakeFrom struct {
	StakeFrom      string `json:"stake_from"`
	ProposalFrozen uint64 `json:"proposal_frozen"`
	VerifyFrozen   uint64 `json:"verify_frozen"`
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
			modelreceipt := convertReceipt(wrapper, block.Hash)
			receipts[i] = modelreceipt
			tx.Status = modelreceipt.Status
		}
	}

	bd := &models.BlockDetail{
		Block:           *block,
		Trans:           trans,
		EvictedReceipts: evictedReceipts,
		Receipts:        receipts,
	}
	return bd, nil
}

func convertReceipt(receipt *types.Receipt, blockHash string) *models.Receipt {
	modelreceipt := &models.Receipt{
		Status:            uint(receipt.Status),
		CumulativeGasUsed: receipt.CumulativeGasUsed,
		Logs:              convertLogs(receipt.Logs, blockHash),
		TxHash:            receipt.TxHash.Hex(),
		ContractAddress:   receipt.ContractAddress.AddrPrefixString(),
	}
	return modelreceipt

}

func convertLogs(logs []*types.Log, blockHash string) []*models.Log {

	if len(logs) > 0 {
		modelsLogs := make([]*models.Log, 0)
		newLog := &models.Log{}
		for _, log := range logs {
			newLog = &models.Log{
				Address:     log.Address.AddrPrefixString(),
				Topic:       log.Topic.Hex(),
				Data:        string(log.Data),
				BlockNumber: log.BlockNumber,
				TxHash:      log.TxHash.Hex(),
				TxIndex:     log.TxIndex,
				BlockHash:   blockHash,
				LogIndex:    log.Index,
				Removed:     log.Removed,
			}
			modelsLogs = append(modelsLogs, newLog)
		}
		return modelsLogs
	}
	return nil
}

func ConvertGroup(g types.GroupI) *models.Group {

	mems := make([]string, 0)
	for _, mem := range g.Members() {
		memberStr := groupsig.DeserializeID(mem.ID()).GetAddrString()
		mems = append(mems, memberStr)
	}
	gh := g.Header()

	data := &Group{
		Seed:          gh.Seed(),
		BeginHeight:   gh.WorkHeight(),
		DismissHeight: gh.DismissHeight(),
		Threshold:     int32(gh.Threshold()),
		Members:       mems,
		MemSize:       len(mems),
		GroupHeight:   gh.GroupHeight(),
	}
	return dataToGroup(data)

}

func dataToGroup(data *Group) *models.Group {

	group := &models.Group{}
	group.Id = data.Seed.Hex()
	group.WorkHeight = data.BeginHeight
	group.DismissHeight = data.DismissHeight
	group.Threshold = uint64(data.Threshold)
	group.Height = data.GroupHeight

	members := data.Members
	group.Members = make([]string, 0)
	group.MemberCount = uint64(len(members))
	for _, midStr := range members {
		if len(midStr) > 0 {
			group.MembersStr = group.MembersStr + midStr + "\r\n"
		}
	}
	return group
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
		Random:     common.ToHex(bh.Random),
		//Qn: mediator.Proc.CalcBlockHeaderQN(bh),
	}
	return block
}
func NewMortGageFromMiner(miner *types.Miner) *MortGage {
	t := "proposal node"
	if miner.IsVerifyRole() {
		t = "verify node"
	}
	status := types.MinerStatusPrepare
	if miner.IsActive() {
		status = types.MinerStatusActive
	} else if miner.IsFrozen() {
		status = types.MinerStatusFrozen
	}

	i := types.MinerNormal
	if miner.IsMinerPool() {
		i = types.MinerPool
	} else if miner.IsInvalidMinerPool() {
		i = types.InValidMinerPool
	} else if miner.IsGuard() {
		i = types.MinerGuard
	}
	mg := &MortGage{
		Stake:                uint64(common.RA2TAS(miner.Stake)),
		ApplyHeight:          miner.ApplyHeight,
		Type:                 t,
		Status:               status,
		StatusUpdateHeight:   miner.StatusUpdateHeight,
		Identity:             i,
		IdentityUpdateHeight: miner.IdentityUpdateHeight,
	}
	return mg
}
func (api *Fetcher) ConvertTempTransactionToTransaction(temp *models.TempTransaction) *models.Transaction {

	tran := &models.Transaction{
		Value:     temp.Value,
		Nonce:     temp.Nonce,
		Type:      int32(temp.Type),
		GasLimit:  temp.GasLimit,
		GasPrice:  temp.GasPrice,
		Hash:      temp.Hash.Hex(),
		ExtraData: temp.ExtraData,
		Status:    temp.Status,
	}
	tran.Data = util.ObjectTojson(temp.Data)
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

func (tm *Fetcher) Fetchbalance(addr string) float64 {
	b := core.BlockChainImpl.GetBalance(common.StringToAddress(addr))
	balance := common.RA2TAS(b.Uint64())

	return balance
}

func HasTransferFunc(code string) bool {
	stringSlice := strings.Split(code, "\n")
	for k, targetString := range stringSlice {
		targetString = strings.TrimSpace(targetString)
		if strings.HasPrefix(targetString, "@register.public") {
			if len(stringSlice) > k+1 {
				if strings.Index(stringSlice[k+1], " transfer(") != -1 {
					return true
				}
			}
		}
	}
	return false
}
func IsTokenContract(contractAddr common.Address) bool {
	chain := core.BlockChainImpl
	db, err := chain.LatestAccountDB()
	if err != nil {
		browserlog.BrowserLog.Error("isTokenContract: ", err)
		return false
	}
	code := db.GetCode(contractAddr)
	contract := tvm.Contract{}
	fmt.Println("IsTokenContract json,", string(code))
	err = json.Unmarshal(code, &contract)
	if err != nil {
		browserlog.BrowserLog.Error("isTokenContract err: ", err)
		return false
	}
	if HasTransferFunc(contract.Code) {
		symbol := db.GetData(contractAddr, []byte("symbol"))
		fmt.Println("IsTokenContract HasTransferFunc,", symbol)

		if len(symbol) >= 1 && symbol[0] == 's' {
			return true
		}
	}
	return false
}

func QueryAccountData(addr string, key string, count int) (interface{}, error) {
	addr = strings.TrimSpace(addr)
	// input check
	if !common.ValidateAddress(addr) {
		return nil, fmt.Errorf("wrong address format")
	}
	address := common.StringToAddress(addr)

	const MaxCountQuery = 100
	if count <= 0 {
		count = 0
	} else if count > MaxCountQuery {
		count = MaxCountQuery
	}

	chain := core.BlockChainImpl
	state, err := chain.GetAccountDBByHash(chain.QueryTopBlock().Hash)
	if err != nil {
		return nil, err
	}

	var resultData interface{}
	if count == 0 {
		value := state.GetData(address, []byte(key))
		if value != nil {
			tmp := make(map[string]interface{})
			tmp["value"] = tvm.VmDataConvert(value)
			resultData = tmp
		}
	} else {
		iter := state.DataIterator(address, []byte(key))
		if iter != nil {
			tmp := make([]map[string]interface{}, 0)
			for iter.Next() {
				k := string(iter.Key[:])
				if !strings.HasPrefix(k, key) {
					continue
				}
				v := tvm.VmDataConvert(iter.Value[:])
				item := make(map[string]interface{}, 0)
				item["key"] = k
				item["value"] = v
				tmp = append(tmp, item)
				resultData = tmp
				if len(tmp) >= count {
					break
				}
			}
		}
	}
	if resultData != nil {
		return resultData, nil
	} else {
		return nil, nil
	}
}
