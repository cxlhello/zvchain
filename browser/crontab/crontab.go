package crontab

import (
	"encoding/json"
	"fmt"
	"github.com/zvchain/zvchain/browser/models"
	"github.com/zvchain/zvchain/browser/mysql"
	"github.com/zvchain/zvchain/browser/util"
	"github.com/zvchain/zvchain/common"
	"github.com/zvchain/zvchain/core"
	"github.com/zvchain/zvchain/middleware/notify"
	"github.com/zvchain/zvchain/middleware/types"
	"sync/atomic"
	"time"
)

const checkInterval = time.Second * 10

type Crontab struct {
	storage           *mysql.Storage
	blockRewardHeight uint64
	blockTopHeight    uint64
	page              uint64
	maxid             uint
	accountPrimaryId  uint64
	isFetchingReward  int32
	isFetchingConsume int32

	isInited            bool
	isFetchingPoolvotes int32
	rpcExplore          *Explore
	transfer            *Transfer
	fetcher             *Fetcher
	isFetchingBlocks    bool
	initdata            chan *models.ForkNotify
}

func NewServer(dbAddr string, dbPort int, dbUser string, dbPassword string, reset bool) *Crontab {
	server := &Crontab{
		initdata: make(chan *models.ForkNotify, 1000),
	}
	server.storage = mysql.NewStorage(dbAddr, dbPort, dbUser, dbPassword, reset)
	server.isInited = true
	notify.BUS.Subscribe(notify.BlockAddSucc, server.OnBlockAddSuccess)
	server.blockRewardHeight = server.storage.TopBlockRewardHeight(mysql.Blockrewardtophight)
	server.blockTopHeight = server.storage.GetTopblock()
	if server.blockRewardHeight > 0 {
		server.blockRewardHeight += 1
	}
	go server.loop()
	return server
}

func (crontab *Crontab) loop() {
	var (
		check = time.NewTicker(checkInterval)
	)
	defer check.Stop()
	go crontab.fetchPoolVotes()
	go crontab.fetchBlockRewards()
	go crontab.Consume()
	for {
		select {
		case <-check.C:
			go crontab.fetchPoolVotes()
			go crontab.fetchBlockRewards()
		}
	}
}

//uopdate invalid guard and pool
func (crontab *Crontab) fetchPoolVotes() {

	if !atomic.CompareAndSwapInt32(&crontab.isFetchingPoolvotes, 0, 1) {
		return
	}
	crontab.excutePoolVotes()
	atomic.CompareAndSwapInt32(&crontab.isFetchingPoolvotes, 1, 0)

}

func (crontab *Crontab) fetchBlockRewards() {
	if !atomic.CompareAndSwapInt32(&crontab.isFetchingReward, 0, 1) {
		return
	}
	crontab.excuteBlockRewards()
	atomic.CompareAndSwapInt32(&crontab.isFetchingReward, 1, 0)
}

func (crontab *Crontab) excutePoolVotes() {
	accountsPool := crontab.storage.GetAccountByRoletype(crontab.maxid, types.MinerPool)
	if accountsPool != nil && len(accountsPool) > 0 {
		blockheader := core.BlockChainImpl.CheckPointAt(mysql.CheckpointMaxHeight)
		var db types.AccountDB
		var err error
		if err != nil || db == nil {
			return
		}
		db, err = core.BlockChainImpl.LatestAccountDB()
		total := len(accountsPool) - 1
		for num, pool := range accountsPool {
			if num == total {
				crontab.maxid = pool.ID
			}
			//pool to be normal miner
			proposalInfo := core.MinerManagerImpl.GetMiner(common.StringToAddress(pool.Address), types.MinerTypeProposal, blockheader.Height)
			attrs := make(map[string]interface{})
			if uint64(proposalInfo.Type) != pool.RoleType {
				attrs["role_type"] = types.InValidMinerPool
			}
			tickets := core.MinerManagerImpl.GetTickets(db, common.StringToAddress(pool.Address))
			var extra = &models.PoolExtraData{}
			if pool.ExtraData != "" {
				if err := json.Unmarshal([]byte(pool.ExtraData), extra); err != nil {
					fmt.Println("Unmarshal json", err.Error())
					if attrs != nil {
						crontab.storage.UpdateAccountByColumn(pool, attrs)
					}
					continue
				}
				//different vote need update
				if extra.Vote != tickets {
					extra.Vote = tickets
					result, _ := json.Marshal(extra)
					attrs["extra_data"] = string(result)
				}
			} else if tickets > 0 {
				extra.Vote = tickets
				result, _ := json.Marshal(extra)
				attrs["extra_data"] = string(result)
			}
			crontab.storage.UpdateAccountByColumn(pool, attrs)
		}
		crontab.excutePoolVotes()
	}
}

func (crontab *Crontab) excuteBlockRewards() {
	height, _ := crontab.storage.TopBlockHeight()
	if crontab.blockRewardHeight > height {
		return
	}
	topblock := core.BlockChainImpl.QueryTopBlock()
	topheight := topblock.Height
	rewards := crontab.rpcExplore.GetPreHightRewardByHeight(crontab.blockRewardHeight)
	fmt.Println("[crontab]  fetchBlockRewards height:", crontab.blockRewardHeight, 0)

	if rewards != nil {
		accounts := crontab.transfer.RewardsToAccounts(rewards)
		if crontab.storage.AddBlockRewardMysqlTransaction(accounts) {
			crontab.blockRewardHeight += 1
		}
		crontab.excuteBlockRewards()
	} else if crontab.blockRewardHeight < topheight {
		crontab.blockRewardHeight += 1
		fmt.Println("[crontab]  fetchBlockRewards rewards nil:", crontab.blockRewardHeight, rewards)
	}
}

func (server *Crontab) consumeBlock(localHeight uint64, pre uint64) {
	fmt.Println("[server]  fetchBlock height:", localHeight)
	var maxHeight uint64
	maxHeight = server.storage.GetTopblock()
	blockDetail, _ := server.fetcher.ExplorerBlockDetail(localHeight)
	if blockDetail != nil {
		if server.storage.AddBlock(&blockDetail.Block) {
			trans := make([]*models.Transaction, 0, 0)
			for i := 0; i < len(blockDetail.Trans); i++ {
				tran := server.fetcher.ConvertTempTransactionToTransaction(blockDetail.Trans[i])
				tran.BlockHash = blockDetail.Block.Hash
				tran.BlockHeight = blockDetail.Block.Height
				tran.CurTime = blockDetail.Block.CurTime
				trans = append(trans, tran)
			}
			server.storage.AddTransactions(trans)
			for i := 0; i < len(blockDetail.Receipts); i++ {
				blockDetail.Receipts[i].BlockHash = blockDetail.Block.Hash
				blockDetail.Receipts[i].BlockHeight = blockDetail.Block.Height
			}
			server.storage.AddReceipts(blockDetail.Receipts)

		}
		if maxHeight > pre {
			server.storage.DeleteForkblock(pre, localHeight)
		}
	}
	//server.isFetchingBlocks = false

}

func (crontab *Crontab) OnBlockAddSuccess(message notify.Message) error {
	block := message.GetData().(*types.Block)
	bh := block.Header

	preHash := bh.PreHash
	preBlock := core.BlockChainImpl.QueryBlockByHash(preHash)
	preHight := preBlock.Header.Height
	fmt.Println("BrowserForkProcessor,pre:", preHight, bh.Height)
	data := &models.ForkNotify{
		PreHeight:   preHight,
		LocalHeight: bh.Height,
	}
	go crontab.Produce(data)
	return nil
}

func (crontab *Crontab) Produce(data *models.ForkNotify) {
	crontab.initdata <- data
	fmt.Println("for Produce", util.ObjectTojson(data))
}

func (crontab *Crontab) Consume() {

	var ok = true
	for ok {
		select {
		case data := <-crontab.initdata:
			crontab.dataCompensationProcess(data.LocalHeight, data.PreHeight)
			crontab.consumeBlock(data.LocalHeight, data.PreHeight)
			fmt.Println("for Consume", util.ObjectTojson(data))
		}
	}
}

func (crontab *Crontab) dataCompensationProcess(notifyHeight uint64, notifyPreHeight uint64) {
	timenow := time.Now()
	if crontab.isInited {
		fmt.Println("[Storage]  dataCompensationProcess start: ", notifyHeight, notifyPreHeight)

		dbMaxHeight := crontab.blockTopHeight
		if dbMaxHeight > 0 && dbMaxHeight <= notifyPreHeight {
			crontab.storage.DeleteForkblock(dbMaxHeight-1, dbMaxHeight+1)
			crontab.dataCompensation(dbMaxHeight, notifyPreHeight)
		}
		crontab.isInited = false
	}
	fmt.Println("[Storage]  dataCompensationProcess cost: ", time.Since(timenow))

}

//data Compensation
func (crontab *Crontab) dataCompensation(dbMaxHeight uint64, notifyPreHeight uint64) {
	blockceil := core.BlockChainImpl.QueryBlockCeil(dbMaxHeight)
	if blockceil != nil {
		preBlockceil := core.BlockChainImpl.QueryBlockByHash(blockceil.Header.PreHash)
		crontab.consumeBlock(blockceil.Header.Height, preBlockceil.Header.Height)
		fmt.Println("for dataCompensation,", blockceil.Header.Height, ",", preBlockceil.Header.Height)
		crontab.blockTopHeight = blockceil.Header.Height + 1
	} else {
		crontab.blockTopHeight += 1
	}
	fmt.Println("[Storage]  dataCompensationProcess procee: ", crontab.blockTopHeight)

	if crontab.blockTopHeight <= notifyPreHeight {
		crontab.dataCompensation(crontab.blockTopHeight, notifyPreHeight)
	}

}
