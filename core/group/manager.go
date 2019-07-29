//   Copyright (C) 2019 ZVChain
//
//   This program is free software: you can redistribute it and/or modify
//   it under the terms of the GNU General Public License as published by
//   the Free Software Foundation, either version 3 of the License, or
//   (at your option) any later version.
//
//   This program is distributed in the hope that it will be useful,
//   but WITHOUT ANY WARRANTY; without even the implied warranty of
//   MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
//   GNU General Public License for more details.
//
//   You should have received a copy of the GNU General Public License
//   along with this program.  If not, see <https://www.gnu.org/licenses/>.

package group

import (
	"fmt"
	"github.com/sirupsen/logrus"
	"github.com/zvchain/zvchain/log"

	"github.com/zvchain/zvchain/common"
	"github.com/zvchain/zvchain/middleware/types"
)

var logger *logrus.Logger

// Manager implements groupContextProvider in package consensus
type Manager struct {
	chain            chainReader
	checkerImpl      types.GroupCreateChecker
	storeReaderImpl  types.GroupStoreReader
	packetSenderImpl types.GroupPacketSender
	minerReaderImpl  minerReader
	poolImpl         *pool
}

func (m *Manager) GetGroupStoreReader() types.GroupStoreReader {
	return m.storeReaderImpl
}

func (m *Manager) GetGroupPacketSender() types.GroupPacketSender {
	return m.packetSenderImpl
}

func (m *Manager) RegisterGroupCreateChecker(checker types.GroupCreateChecker) {
	m.checkerImpl = checker
}

func NewManager(chain chainReader) Manager {
	logger = log.GroupLogger
	gPool := newPool()
	store := NewStore(chain, gPool)
	packetSender := NewPacketSender(chain)

	managerImpl := Manager{
		chain:            chain,
		storeReaderImpl:  store,
		packetSenderImpl: packetSender,
		poolImpl:         gPool,
		//minerReaderImpl:  reader,
	}
	return managerImpl
}

func (m *Manager) InitManager(minerReader minerReader, gen *types.GenesisInfo) {
	m.minerReaderImpl = minerReader
	db, err := m.chain.LatestStateDB()
	if err != nil {
		panic(fmt.Sprintf("failed to init group manager pool %v", err))
	}
	err = m.poolImpl.initPool(db, gen)
	if err != nil {
		panic(fmt.Sprintf("failed to init group manager pool %v", err))
	}

}

func (m *Manager) InitGenesis(db types.AccountDB, genesisInfo *types.GenesisInfo) {
	err := m.poolImpl.initGenesis(db, genesisInfo)
	if err != nil {
		panic(fmt.Sprintf("failed to init InitGenesis %v", err))
	}
}

// RegularCheck try to create group, do punishment and refresh active group
func (m *Manager) RegularCheck(db types.AccountDB, bh *types.BlockHeader) {
	ctx := &CheckerContext{bh.Height}
	m.tryCreateGroup(db, m.checkerImpl, ctx)
	m.tryDoPunish(db, m.checkerImpl, ctx)
}

// GroupCreatedInCurrentBlock returns the group data if group is created in current block
func (m *Manager) GroupCreatedInCurrentBlock(block *types.Block) *group {
	db, err := m.chain.LatestStateDB()
	if err != nil {
		logger.Error("failed to get state db in GroupCreatedInCurrentBlock", err)
	}
	topGroup := m.poolImpl.getTopGroup(db)
	if topGroup.HeaderD.BlockHeight == block.Header.Height {
		logger.Debugf("Notify consensus as group created on %v", topGroup.HeaderD.BlockHeight)
		logger.Debugf("Member number is  %d", len(topGroup.members))
		// group just created
		return topGroup
	}
	return nil
}

// ResetTop resets group with top block with parameter bh
func (m *Manager) ResetToTop(db types.AccountDB, bh *types.BlockHeader) {
	m.poolImpl.resetToTop(db, bh.Height)
}

// Height returns count of current group number
func (m *Manager) Height() uint64 {
	db, err := m.chain.LatestStateDB()
	if err != nil {
		logger.Error("failed to get last db")
		return 0
	}
	return m.poolImpl.count(db)
}

func (m *Manager) GroupsAfter(height uint64) []types.GroupI {
	return m.poolImpl.groupsAfter(m.chain, height, common.MaxInt64)
}

// Height returns count of current group number
func (m *Manager) ActiveGroupCount() int {
	return len(m.poolImpl.getActives(m.chain, m.chain.Height()))
	//return len(m.poolImpl.activeList)
}

// GetAvailableGroupSeeds gets available groups' Seed at the given Height
func (m *Manager) GetAvailableGroupSeeds(height uint64) []types.SeedI {
	return m.storeReaderImpl.GetAvailableGroupSeeds(height)
}

// GetGroupBySeed returns the group info of the given Seed
func (m *Manager) GetGroupBySeed(seedHash common.Hash) types.GroupI {
	g := m.storeReaderImpl.GetGroupBySeed(seedHash)
	if g == nil {
		return nil
	}
	return g
}

// GetGroupHeaderBySeed returns the group header info of the given Seed
func (m *Manager) GetGroupHeaderBySeed(seedHash common.Hash) types.GroupHeaderI {
	gh := m.storeReaderImpl.GetGroupHeaderBySeed(seedHash)
	if gh == nil {
		return nil
	}
	return gh
}

func (m *Manager) GetLivedGroupsByMember(address common.Address, height uint64) []types.GroupI {
	groups := m.poolImpl.getLives(m.chain, height)
	groupIs := make([]types.GroupI, 0)
	for _, g := range groups {
		if g.hasMember(address.Bytes()) {
			groupIs = append(groupIs, g)
		}
	}
	return groupIs
}

func (m *Manager) tryCreateGroup(db types.AccountDB, checker types.GroupCreateChecker, ctx types.CheckerContext) {
	createResult := checker.CheckGroupCreateResult(ctx)
	if createResult == nil {
		return
	}
	//if createResult.Err() != nil {
	//	return
	//}
	switch createResult.Code() {
	case types.CreateResultSuccess:
		err := m.saveGroup(db, newGroup(createResult.GroupInfo(), ctx.Height(), m.poolImpl.getTopGroup(db)))
		if err != nil {
			// this case must not happen.
			logger.Panicf("saveGroup error: %v", err)
		}
	case types.CreateResultMarkEvil:
		markGroupFail(db, createResult)
	case types.CreateResultFail:
		// do nothing
	}
	if len(createResult.FrozenMiners()) > 0 {
		m.frozeMiner(db, createResult.FrozenMiners(), ctx)
	}

}

func (m *Manager) tryDoPunish(db types.AccountDB, checker types.GroupCreateChecker, ctx types.CheckerContext) {
	msg, err := checker.CheckGroupCreatePunishment(ctx)
	if err != nil {
		return
	}
	_, err = m.minerReaderImpl.MinerPenalty(db, msg, ctx.Height())
	if err != nil {
		logger.Errorf("MinerPenalty error: %v", err)
	}
}

func (m *Manager) saveGroup(db types.AccountDB, group *group) error {
	return m.poolImpl.add(db, group)
}

func (m *Manager) frozeMiner(db types.AccountDB, frozenMiners [][]byte, ctx types.CheckerContext) {
	logger.Debugf("frozeMiner: %v", frozenMiners)
	for _, p := range frozenMiners {
		addr := common.BytesToAddress(p)
		_, err := m.minerReaderImpl.MinerFrozen(db, addr, ctx.Height())
		if err != nil {
			logger.Errorf("MinerFrozen error: %v", err)
		}
	}
}

// markGroupFail mark group member should upload origin piece
func markGroupFail(db types.AccountDB, seed types.SeedI) {
	db.SetData(common.HashToAddress(seed.Seed()), originPieceReqKey, []byte{1})
}
