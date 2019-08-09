//   Copyright (C) 2018 ZVChain
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

/*
	Package cli provides client command line window
*/
package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/zvchain/zvchain/common"
	"github.com/zvchain/zvchain/consensus/model"
	"github.com/zvchain/zvchain/storage/tasdb"
	"golang.org/x/crypto/scrypt"

	"github.com/syndtr/goleveldb/leveldb/filter"
	"github.com/syndtr/goleveldb/leveldb/opt"
)

const (
	statusLocked   int8 = 0
	statusUnLocked      = 1
)
const DefaultPassword = "123"

type AccountManager struct {
	store    *tasdb.LDBDatabase
	accounts sync.Map

	unlockAccount *AccountInfo
	mu            sync.Mutex
}

type AccountInfo struct {
	Account
	Status       int8
	UnLockExpire time.Time
}

func (ai *AccountInfo) unlocked() bool {
	return time.Now().Before(ai.UnLockExpire) && ai.Status == statusUnLocked
}

func (ai *AccountInfo) resetExpireTime() {
	//ai.UnLockExpire = time.Now().Add(time.Duration(120) * time.Second)
}

type KeyStoreRaw struct {
	Key     []byte
	IsMiner bool
}

type Account struct {
	Address  string
	Pk       string
	Sk       string
	Password string
	Miner    *MinerRaw
}

type MinerRaw struct {
	BPk   string
	BSk   string
	VrfPk string
	VrfSk string
}

func dirExists(dir string) bool {
	f, err := os.Stat(dir)
	if err != nil {
		return false
	}
	return f.IsDir()
}

func (a *Account) MinerSk() string {
	return a.Sk
}

func newAccountOp(ks string) (*AccountManager, error) {
	options := &opt.Options{
		OpenFilesCacheCapacity:        10,
		WriteBuffer:                   8 * opt.MiB, // Two of these are used internally
		Filter:                        filter.NewBloomFilter(10),
		CompactionTableSize:           2 * opt.MiB,
		CompactionTableSizeMultiplier: 2,
	}
	db, err := tasdb.NewLDBDatabase(ks, options)
	if err != nil {
		return nil, fmt.Errorf("new ldb fail:%v", err.Error())
	}
	return &AccountManager{
		store: db,
	}, nil
}

func initAccountManager(keystore string, readyOnly bool) (accountOp, error) {
	// Specify internal account creation when you deploy in bulk (just create it once)
	if readyOnly && !dirExists(keystore) {
		aop, err := newAccountOp(keystore)
		if err != nil {
			return nil, err
		}

		ret := aop.NewAccount(DefaultPassword, true)
		if !ret.IsSuccess() {
			fmt.Println(ret.Message)
			return nil, err
		}
		return aop, nil
	}

	aop, err := newAccountOp(keystore)
	if err != nil {
		return nil, err
	}
	return aop, nil
}

func (am *AccountManager) constructAccount(password string, sk *common.PrivateKey, bMiner bool) (*Account, error) {
	account := &Account{
		Sk:       sk.Hex(),
		Pk:       sk.GetPubKey().Hex(),
		Address:  sk.GetPubKey().GetAddress().AddrString(),
		Password: passwordHash(password),
	}

	if bMiner {
		minerDO, err := model.NewSelfMinerDO(sk)
		if err != nil {
			return nil, err
		}

		minerRaw := &MinerRaw{
			BPk:   minerDO.PK.GetHexString(),
			BSk:   minerDO.SK.GetHexString(),
			VrfPk: minerDO.VrfPK.GetHexString(),
			VrfSk: minerDO.VrfSK.GetHexString(),
		}
		account.Miner = minerRaw
	}
	return account, nil
}

func (am *AccountManager) loadAccount(addr string, password string) (*Account, error) {
	v, err := am.store.Get([]byte(addr))
	if err != nil {
		return nil, err
	}

	salt := common.Sha256([]byte(password))
	scryptPwd, err := scrypt.Key([]byte(password), salt, 1<<15, 8, 1, 32)
	if err != nil {
		return nil, err
	}

	bs, err := common.DecryptWithKey(scryptPwd, v)
	if err != nil {
		return nil, err
	}

	var ksr = new(KeyStoreRaw)
	if err = json.Unmarshal(bs, ksr); err != nil {
		return nil, err
	}

	secKey := new(common.PrivateKey)
	if !secKey.ImportKey(ksr.Key) {
		return nil, ErrInternal
	}

	return am.constructAccount(password, secKey, ksr.IsMiner)
}

func (am *AccountManager) storeAccount(addr string, ksr *KeyStoreRaw, password string) error {
	bs, err := json.Marshal(ksr)
	if err != nil {
		return err
	}

	salt := common.Sha256([]byte(password))
	scryptPwd, err := scrypt.Key([]byte(password), salt, 1<<15, 8, 1, 32)
	if err != nil {
		return err
	}
	ct, err := common.EncryptWithKey(scryptPwd, bs)
	if err != nil {
		return err
	}

	err = am.store.Put([]byte(addr), ct)
	return err
}

func (am *AccountManager) getFirstMinerAccount(password string) *Account {
	iter := am.store.NewIterator()
	for iter.Next() {
		addr := string(iter.Key())
		if v, ok := am.accounts.Load(addr); ok {
			aci := v.(*AccountInfo)
			if passwordHash(password) == aci.Password && aci.Miner != nil {
				return &aci.Account
			}
		} else {
			acc, err := am.loadAccount(addr, password)
			if err == nil && acc.Miner != nil {
				return acc
			}
		}
	}
	return nil
}

func (am *AccountManager) checkMinerAccount(addr string, password string) (*AccountInfo, error) {
	var aci *AccountInfo
	if v, ok := am.accounts.Load(addr); ok {
		aci = v.(*AccountInfo)
		if passwordHash(password) != aci.Password {
			return nil, ErrPassword
		}
	} else {
		acc, err := am.loadAccount(addr, password)
		if err != nil {
			return nil, err
		}
		aci = &AccountInfo{
			Account: *acc,
		}
		am.accounts.Store(addr, aci)
	}
	return aci, nil
}

func (am *AccountManager) resetExpireTime(addr string) {
	acc, err := am.getAccountInfo(addr)
	if err != nil {
		return
	}
	acc.resetExpireTime()
}

func (am *AccountManager) getAccountInfo(addr string) (*AccountInfo, error) {
	var aci *AccountInfo
	if v, ok := am.accounts.Load(addr); ok {
		aci = v.(*AccountInfo)
		return aci, nil
	}
	return nil, ErrUnlocked
}

func (am *AccountManager) currentUnLockedAddr() string {
	if am.unlockAccount != nil && am.unlockAccount.unlocked() {
		return am.unlockAccount.Address
	}
	return ""
}

func passwordHash(password string) string {
	return common.ToHex(common.Sha256([]byte(password)))
}

// NewAccount create a new account by password
func (am *AccountManager) NewAccount(password string, miner bool) *Result {
	privateKey, err := common.GenerateKey("")
	if err != nil {
		return opError(err)
	}
	account, err := am.constructAccount(password, &privateKey, miner)
	if err != nil {
		return opError(err)
	}

	ksr := &KeyStoreRaw{
		Key:     privateKey.ExportKey(),
		IsMiner: miner,
	}
	if err := am.storeAccount(account.Address, ksr, password); err != nil {
		return opError(err)
	}
	aci := &AccountInfo{
		Account: *account,
	}
	am.accounts.Store(account.Address, aci)

	return opSuccess(account.Address)
}

// AccountList show account list
func (am *AccountManager) AccountList() *Result {
	iter := am.store.NewIterator()
	addrs := make([]string, 0)
	for iter.Next() {
		addrs = append(addrs, string(iter.Key()))
	}
	return opSuccess(addrs)
}

// Lock lock the account by address
func (am *AccountManager) Lock(addr string) *Result {
	aci, err := am.getAccountInfo(addr)
	if err != nil {
		return opError(err)
	}
	aci.Status = statusLocked
	return opSuccess(nil)
}

// UnLock unlock the account by address and password
func (am *AccountManager) UnLock(addr string, password string, duration uint) *Result {
	var aci *AccountInfo
	if v, ok := am.accounts.Load(addr); ok {
		aci = v.(*AccountInfo)
		if passwordHash(password) != aci.Password {
			return opError(ErrPassword)
		}
	} else {
		acc, err := am.loadAccount(addr, password)
		if err != nil {
			return opError(ErrPassword)
		}
		aci = &AccountInfo{
			Account: *acc,
		}
		am.accounts.Store(addr, aci)
	}

	am.mu.Lock()
	defer am.mu.Unlock()

	if am.unlockAccount != nil && aci.Address != am.unlockAccount.Address {
		am.unlockAccount.Status = statusLocked
	}

	aci.Status = statusUnLocked
	aci.UnLockExpire = time.Now().Add(time.Duration(duration) * time.Second)
	am.unlockAccount = aci

	return opSuccess(nil)
}

// AccountInfo show account info
func (am *AccountManager) AccountInfo() *Result {
	addr := am.currentUnLockedAddr()
	if addr == "" {
		return opError(ErrUnlocked)
	}
	aci, err := am.getAccountInfo(addr)
	if err != nil {
		return opError(err)
	}
	if !aci.unlocked() {
		return opError(ErrUnlocked)
	}
	aci.resetExpireTime()
	return opSuccess(&aci.Account)
}

// DeleteAccount delete current unlocked account
func (am *AccountManager) DeleteAccount() *Result {
	addr := am.currentUnLockedAddr()
	if addr == "" {
		return opError(ErrUnlocked)
	}
	aci, err := am.getAccountInfo(addr)
	if err != nil {
		return opError(err)
	}
	if !aci.unlocked() {
		return opError(ErrUnlocked)
	}
	am.accounts.Delete(addr)
	am.store.Delete([]byte(addr))
	return opSuccess(nil)
}

func (am *AccountManager) Close() {
	am.store.Close()
}

// NewAccountByImportKey create a new account by the input private key
func (am *AccountManager) NewAccountByImportKey(key string, password string, miner bool) *Result {
	kBytes := common.FromHex(key)
	privateKey := new(common.PrivateKey)
	if !privateKey.ImportKey(kBytes) {
		return opError(ErrInternal)
	}

	account, err := am.constructAccount(password, privateKey, miner)
	if err != nil {
		return opError(err)
	}

	ksr := &KeyStoreRaw{
		Key:     kBytes,
		IsMiner: miner,
	}
	if err := am.storeAccount(account.Address, ksr, password); err != nil {
		return opError(err)
	}
	aci := &AccountInfo{
		Account: *account,
	}
	am.accounts.Store(account.Address, aci)

	return opSuccess(account.Address)
}

// ExportKey exports the private key of account
func (am *AccountManager) ExportKey(addr string) *Result {
	acc, err := am.getAccountInfo(addr)
	if err != nil {
		return opError(err)
	}
	sk := common.HexToSecKey(acc.Sk)
	return opSuccess(common.ToHex(sk.ExportKey()))
}
