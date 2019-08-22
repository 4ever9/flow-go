package core

import (
	"reflect"
	"sync"
	"time"

	crypto "github.com/dapperlabs/bamboo-node/pkg/crypto/oldcrypto"
	"github.com/dapperlabs/bamboo-node/pkg/language/runtime"
	"github.com/dapperlabs/bamboo-node/pkg/types"

	eruntime "github.com/dapperlabs/bamboo-node/internal/emulator/runtime"
	"github.com/dapperlabs/bamboo-node/internal/emulator/state"
	etypes "github.com/dapperlabs/bamboo-node/internal/emulator/types"
)

// EmulatedBlockchain simulates a blockchain in the background to enable easy smart contract testing.
//
// Contains a versioned World State store and a pending transaction pool for granular state update tests.
// Both "committed" and "intermediate" world states are logged for query by version (state hash) utilities,
// but only "committed" world states are enabled for the SeekToState feature.
type EmulatedBlockchain struct {
	// mapping of committed world states (updated after CommitBlock)
	worldStates map[crypto.Hash][]byte
	// mapping of intermediate world states (updated after SubmitTransaction)
	intermediateWorldStates map[crypto.Hash][]byte
	// current world state
	pendingWorldState *state.WorldState
	// pool of pending transactions waiting to be commmitted (already executed)
	txPool             map[crypto.Hash]*types.SignedTransaction
	mutex              sync.RWMutex
	computer           *Computer
	rootAccountAddress types.Address
	rootAccountKeyPair *crypto.KeyPair
}

// EmulatedBlockchainOptions is a set of configuration options for an emulated blockchain.
type EmulatedBlockchainOptions struct {
	RootAccountKeyPair *crypto.KeyPair
}

// DefaultOptions is the default configuration for an emulated blockchain.
var DefaultOptions = &EmulatedBlockchainOptions{}

// NewEmulatedBlockchain instantiates a new blockchain backend for testing purposes.
func NewEmulatedBlockchain(opt *EmulatedBlockchainOptions) *EmulatedBlockchain {
	worldStates := make(map[crypto.Hash][]byte)
	intermediateWorldStates := make(map[crypto.Hash][]byte)
	txPool := make(map[crypto.Hash]*types.SignedTransaction)
	computer := NewComputer(runtime.NewInterpreterRuntime())
	ws := state.NewWorldState()

	rootAccountAddress, rootAccountKeyPair := createRootAccount(ws, opt.RootAccountKeyPair)

	bytes := ws.Encode()
	worldStates[ws.Hash()] = bytes

	return &EmulatedBlockchain{
		worldStates:             worldStates,
		intermediateWorldStates: intermediateWorldStates,
		pendingWorldState:       ws,
		txPool:                  txPool,
		computer:                computer,
		rootAccountAddress:      rootAccountAddress,
		rootAccountKeyPair:      rootAccountKeyPair,
	}
}

func (b *EmulatedBlockchain) RootAccount() types.Address {
	return b.rootAccountAddress
}

func (b *EmulatedBlockchain) RootKeyPair() *crypto.KeyPair {
	return b.rootAccountKeyPair
}

// GetLatestBlock gets the latest sealed block.
func (b *EmulatedBlockchain) GetLatestBlock() *etypes.Block {
	return b.pendingWorldState.GetLatestBlock()
}

// GetBlockByHash gets a block by hash.
func (b *EmulatedBlockchain) GetBlockByHash(hash crypto.Hash) (*etypes.Block, error) {
	block := b.pendingWorldState.GetBlockByHash(hash)
	if block == nil {
		return nil, &ErrBlockNotFound{BlockHash: hash}
	}

	return block, nil
}

// GetBlockByNumber gets a block by number.
func (b *EmulatedBlockchain) GetBlockByNumber(number uint64) (*etypes.Block, error) {
	block := b.pendingWorldState.GetBlockByNumber(number)
	if block == nil {
		return nil, &ErrBlockNotFound{BlockNum: number}
	}

	return block, nil
}

// GetTransaction gets an existing transaction by hash.
//
// First looks in pending txPool, then looks in current blockchain state.
func (b *EmulatedBlockchain) GetTransaction(txHash crypto.Hash) (*types.SignedTransaction, error) {
	b.mutex.RLock()
	defer b.mutex.RUnlock()

	if tx, ok := b.txPool[txHash]; ok {
		return tx, nil
	}

	tx := b.pendingWorldState.GetTransaction(txHash)
	if tx == nil {
		return nil, &ErrTransactionNotFound{TxHash: txHash}
	}

	return tx, nil
}

// GetTransactionAtVersion gets an existing transaction by hash at a specified state.
func (b *EmulatedBlockchain) GetTransactionAtVersion(txHash, version crypto.Hash) (*types.SignedTransaction, error) {
	ws, err := b.getWorldStateAtVersion(version)
	if err != nil {
		return nil, err
	}

	tx := ws.GetTransaction(txHash)
	if tx == nil {
		return nil, &ErrTransactionNotFound{TxHash: txHash}
	}

	return tx, nil
}

// GetAccount gets account information associated with an address identifier.
func (b *EmulatedBlockchain) GetAccount(address types.Address) (*types.Account, error) {
	registers := b.pendingWorldState.Registers.NewView()
	runtimeAPI := eruntime.NewEmulatorRuntimeAPI(registers)
	account := runtimeAPI.GetAccount(address)
	if account == nil {
		return nil, &ErrAccountNotFound{Address: address}
	}

	return account, nil
}

// GetAccountAtVersion gets account information associated with an address identifier at a specified state.
func (b *EmulatedBlockchain) GetAccountAtVersion(address types.Address, version crypto.Hash) (*types.Account, error) {
	ws, err := b.getWorldStateAtVersion(version)
	if err != nil {
		return nil, err
	}

	registers := ws.Registers.NewView()
	runtimeAPI := eruntime.NewEmulatorRuntimeAPI(registers)
	account := runtimeAPI.GetAccount(address)
	if account == nil {
		return nil, &ErrAccountNotFound{Address: address}
	}

	return account, nil
}

// SubmitTransaction sends a transaction to the network that is immediately executed (updates blockchain state).
//
// Note that the resulting state is not finalized until CommitBlock() is called.
// However, the pending blockchain state is indexed for testing purposes.
func (b *EmulatedBlockchain) SubmitTransaction(tx *types.SignedTransaction) error {
	b.mutex.Lock()
	defer b.mutex.Unlock()

	if _, exists := b.txPool[tx.Hash()]; exists {
		return &ErrDuplicateTransaction{TxHash: tx.Hash()}
	}

	if b.pendingWorldState.ContainsTransaction(tx.Hash()) {
		return &ErrDuplicateTransaction{TxHash: tx.Hash()}
	}

	if err := b.validateSignature(tx.PayerSignature); err != nil {
		return err
	}

	b.txPool[tx.Hash()] = tx
	b.pendingWorldState.InsertTransaction(tx)

	registers := b.pendingWorldState.Registers.NewView()
	err := b.computer.ExecuteTransaction(tx, registers)
	if err != nil {
		b.pendingWorldState.UpdateTransactionStatus(tx.Hash(), types.TransactionReverted)

		b.updatePendingWorldStates(tx.Hash())

		return &ErrTransactionReverted{TxHash: tx.Hash(), Err: err}
	}

	b.pendingWorldState.SetRegisters(registers.UpdatedRegisters())
	b.pendingWorldState.UpdateTransactionStatus(tx.Hash(), types.TransactionFinalized)

	b.updatePendingWorldStates(tx.Hash())

	return nil
}

func (b *EmulatedBlockchain) updatePendingWorldStates(txHash crypto.Hash) {
	if _, exists := b.intermediateWorldStates[txHash]; exists {
		return
	}

	bytes := b.pendingWorldState.Encode()
	b.intermediateWorldStates[txHash] = bytes
}

// CallScript executes a read-only script against the world state and returns the result.
func (b *EmulatedBlockchain) CallScript(script []byte) (interface{}, error) {
	registers := b.pendingWorldState.Registers.NewView()
	runtimeAPI := eruntime.NewEmulatorRuntimeAPI(registers)
	return b.computer.ExecuteScript(script, runtimeAPI)
}

// CallScriptAtVersion executes a read-only script against a specified world state and returns the result.
func (b *EmulatedBlockchain) CallScriptAtVersion(script []byte, version crypto.Hash) (interface{}, error) {
	ws, err := b.getWorldStateAtVersion(version)
	if err != nil {
		return nil, err
	}

	registers := ws.Registers.NewView()
	runtimeAPI := eruntime.NewEmulatorRuntimeAPI(registers)
	return b.computer.ExecuteScript(script, runtimeAPI)
}

// CommitBlock takes all pending transactions and commits them into a block.
//
// Note that this clears the pending transaction pool and indexes the committed
// blockchain state for testing purposes.
func (b *EmulatedBlockchain) CommitBlock() *etypes.Block {
	b.mutex.Lock()
	defer b.mutex.Unlock()

	txHashes := make([]crypto.Hash, 0)
	for hash := range b.txPool {
		txHashes = append(txHashes, hash)
		if b.pendingWorldState.GetTransaction(hash).Status != types.TransactionReverted {
			b.pendingWorldState.UpdateTransactionStatus(hash, types.TransactionSealed)
		}
	}
	b.txPool = make(map[crypto.Hash]*types.SignedTransaction)

	prevBlock := b.pendingWorldState.GetLatestBlock()
	block := &etypes.Block{
		Number:            prevBlock.Number + 1,
		Timestamp:         time.Now(),
		PreviousBlockHash: prevBlock.Hash(),
		TransactionHashes: txHashes,
	}

	b.pendingWorldState.InsertBlock(block)
	b.commitWorldState(block.Hash())
	return block
}

// SeekToState rewinds the blockchain state to a previously committed history.
//
// Note that this only seeks to a committed world state (not intermediate world state)
// and this clears all pending transactions in txPool.
func (b *EmulatedBlockchain) SeekToState(hash crypto.Hash) {
	b.mutex.Lock()
	defer b.mutex.Unlock()

	if bytes, ok := b.worldStates[hash]; ok {
		ws := state.Decode(bytes)
		b.pendingWorldState = ws
		b.txPool = make(map[crypto.Hash]*types.SignedTransaction)
	}
}

func (b *EmulatedBlockchain) getWorldStateAtVersion(wsHash crypto.Hash) (*state.WorldState, error) {
	if wsBytes, ok := b.worldStates[wsHash]; ok {
		return state.Decode(wsBytes), nil
	}

	if wsBytes, ok := b.intermediateWorldStates[wsHash]; ok {
		return state.Decode(wsBytes), nil
	}

	return nil, &ErrInvalidStateVersion{Version: wsHash}
}

func (b *EmulatedBlockchain) commitWorldState(blockHash crypto.Hash) {
	if _, exists := b.worldStates[blockHash]; exists {
		return
	}

	bytes := b.pendingWorldState.Encode()
	b.worldStates[blockHash] = bytes
}

func (b *EmulatedBlockchain) validateSignature(signature types.AccountSignature) error {
	account, err := b.GetAccount(signature.Account)
	if err != nil {
		return &ErrInvalidSignatureAccount{Account: signature.Account}
	}

	for _, publicKey := range account.PublicKeys {
		// TODO: perform real signature verification
		if reflect.DeepEqual(publicKey, signature.PublicKey) {
			return nil
		}
	}

	return &ErrInvalidSignaturePublicKey{
		Account:   signature.Account,
		PublicKey: signature.PublicKey,
	}
}

// createRootAccount creates a new root account and commits it to the world state.
func createRootAccount(ws *state.WorldState, keyPair *crypto.KeyPair) (types.Address, *crypto.KeyPair) {
	registers := ws.Registers.NewView()

	if keyPair == nil {
		keyPair, _ = crypto.KeyPairFromSeed("root")
	}

	runtimeAPI := eruntime.NewEmulatorRuntimeAPI(registers)
	accountID, _ := runtimeAPI.CreateAccount(keyPair.PublicKey, []byte{})

	ws.SetRegisters(registers.UpdatedRegisters())

	return types.BytesToAddress(accountID), keyPair
}
