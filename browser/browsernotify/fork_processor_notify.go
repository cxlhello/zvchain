package notify

import (
	"fmt"
	"github.com/zvchain/zvchain/browser/mysql"
	"github.com/zvchain/zvchain/core"
	"github.com/zvchain/zvchain/middleware/notify"
	"github.com/zvchain/zvchain/middleware/types"
)

type BrowserForkProcessor struct {
	storage *mysql.Storage
}

func (fork *BrowserForkProcessor) OnBlockAddSuccess(message notify.Message) error {

	block := message.GetData().(*types.Block)
	bh := block.Header
	preHash := bh.PreHash
	preBlock := core.BlockChainImpl.QueryBlockByHash(preHash)
	preHight := preBlock.Header.Height
	fmt.Println("BrowserForkProcessor,pre:", preHight, bh.Height)
	return fork.storage.DeleteForkblock(preHight, bh.Height)

}
