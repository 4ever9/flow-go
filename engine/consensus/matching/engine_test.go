// (c) 2019 Dapper Labs - ALL RIGHTS RESERVED

package matching

import (
	"os"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"
	"go.uber.org/atomic"

	"github.com/onflow/flow-go/engine"
	"github.com/onflow/flow-go/model/chunks"
	"github.com/onflow/flow-go/model/flow"
	"github.com/onflow/flow-go/model/messages"
	"github.com/onflow/flow-go/module/metrics"
	mockmodule "github.com/onflow/flow-go/module/mock"
	"github.com/onflow/flow-go/module/validation"
	"github.com/onflow/flow-go/network/mocknetwork"
	"github.com/onflow/flow-go/utils/unittest"
)

// 1. Matching engine should validate the incoming receipt (aka ExecutionReceipt):
//     1. it should stores it to the mempool if valid
//     2. it should ignore it when:
//         1. the origin is invalid [Condition removed for now -> will be replaced by valid EN signature in future]
//         2. the role is invalid
//         3. the result (a receipt has one result, multiple receipts might have the same result) has been sealed already
//         4. the receipt has been received before
//         5. the result has been received before
// 2. Matching engine should validate the incoming approval (aka ResultApproval):
//     1. it should store it to the mempool if valid
//     2. it should ignore it when:
//         1. the origin is invalid
//         2. the role is invalid
//         3. the result has been sealed already
// 3. Matching engine should be able to find matched results:
//     1. It should find no matched result if there is no result and no approval
//     2. it should find 1 matched result if we received a receipt, and the block has no payload (impossible now, system every block will have at least one chunk to verify)
//     3. It should find no matched result if there is only result, but no approval (skip for now, because we seal results without approvals)
// 4. Matching engine should be able to seal a matched result:
//     1. It should not seal a matched result if:
//         1. the block is missing (consensus hasn’t received this executed block yet)
//         2. the approvals for a certain chunk are insufficient (skip for now, because we seal results without approvals)
//         3. there is some chunk didn’t receive enough approvals
//         4. the previous result is not known
//         5. the previous result references the wrong block
//     2. It should seal a matched result if the approvals are sufficient
// 5. Matching engine should request results from execution nodes:
//     1. If there are unsealed and finalized blocks, it should request the execution receipts from the execution nodes.
func TestMatchingEngine(t *testing.T) {
	suite.Run(t, new(MatchingSuite))
}

type MatchingSuite struct {
	unittest.BaseChainSuite
	// misc SERVICE COMPONENTS which are injected into Matching Engine
	requester        *mockmodule.Requester
	receiptValidator *mockmodule.ReceiptValidator

	// MATCHING ENGINE
	matching *Engine
}

func (ms *MatchingSuite) SetupTest() {
	// ~~~~~~~~~~~~~~~~~~~~~~~~~~ SETUP SUITE ~~~~~~~~~~~~~~~~~~~~~~~~~~ //
	ms.SetupChain()

	unit := engine.NewUnit()
	log := zerolog.New(os.Stderr)
	metrics := metrics.NewNoopCollector()

	// ~~~~~~~~~~~~~~~~~~~~~~~ SETUP MATCHING ENGINE ~~~~~~~~~~~~~~~~~~~~~~~ //
	ms.requester = new(mockmodule.Requester)
	ms.receiptValidator = &mockmodule.ReceiptValidator{}

	ms.matching = &Engine{
		unit:                      unit,
		log:                       log,
		engineMetrics:             metrics,
		mempool:                   metrics,
		metrics:                   metrics,
		state:                     ms.State,
		receiptRequester:          ms.requester,
		resultsDB:                 ms.ResultsDB,
		headersDB:                 ms.HeadersDB,
		indexDB:                   ms.IndexDB,
		incorporatedResults:       ms.ResultsPL,
		receipts:                  ms.ReceiptsPL,
		approvals:                 ms.ApprovalsPL,
		seals:                     ms.SealsPL,
		isCheckingSealing:         atomic.NewBool(false),
		sealingThreshold:          10,
		maxResultsToRequest:       200,
		assigner:                  ms.Assigner,
		receiptValidator:          ms.receiptValidator,
		requestTracker:            NewRequestTracker(1, 3),
		approvalRequestsThreshold: 10,
		requiredChunkApprovals:    validation.DefaultRequiredChunkApprovals,
	}
}

// Test that we reject receipts for unknown blocks without generating an error
func (ms *MatchingSuite) TestOnReceiptUnknownBlock() {
	// This receipt has a random block ID, so the matching engine won't find it.
	receipt := unittest.ExecutionReceiptFixture()

	// onReceipt should reject the receipt without throwing an error
	err := ms.matching.onReceipt(receipt.ExecutorID, receipt)
	ms.Require().NoError(err, "should drop receipt for unknown block without error")

	ms.ReceiptsPL.AssertNumberOfCalls(ms.T(), "Add", 0)
	ms.ResultsPL.AssertNumberOfCalls(ms.T(), "Add", 0)
}

// matching engine should drop Result for known block that is already sealed
// without trying to store anything
func (ms *MatchingSuite) TestOnReceiptSealedResult() {
	originID := ms.ExeID
	receipt := unittest.ExecutionReceiptFixture(
		unittest.WithExecutorID(originID),
		unittest.WithResult(unittest.ExecutionResultFixture(unittest.WithBlock(&ms.LatestSealedBlock))),
	)

	err := ms.matching.onReceipt(originID, receipt)
	ms.Require().NoError(err, "should ignore receipt for sealed result")

	ms.ResultsDB.AssertNumberOfCalls(ms.T(), "Store", 0)
	ms.ResultsPL.AssertNumberOfCalls(ms.T(), "Add", 0)
}

// Test that we reject receipts that are already pooled
func (ms *MatchingSuite) TestOnReceiptPendingReceipt() {
	receipt := unittest.ExecutionReceiptFixture(
		unittest.WithExecutorID(ms.ExeID),
		unittest.WithResult(unittest.ExecutionResultFixture(unittest.WithBlock(&ms.UnfinalizedBlock))),
	)

	ms.receiptValidator.On("Validate", receipt).Return(nil)

	// setup the receipts mempool to check if we attempted to add the receipt to
	// the mempool, and return false as we are testing the case where it was already in the mempool
	ms.ReceiptsPL.On("Add", receipt).Return(false).Once()

	// onReceipt should return immediately after trying to insert the receipt,
	// but without throwing any errors
	err := ms.matching.onReceipt(receipt.ExecutorID, receipt)
	ms.Require().NoError(err, "should ignore already pending receipt")

	ms.ReceiptsPL.AssertExpectations(ms.T())
	ms.ResultsPL.AssertNumberOfCalls(ms.T(), "Add", 0)
}

// try to submit a receipt for an already received result
func (ms *MatchingSuite) TestOnReceiptPendingResult() {
	originID := ms.ExeID
	receipt := unittest.ExecutionReceiptFixture(
		unittest.WithExecutorID(originID),
		unittest.WithResult(unittest.ExecutionResultFixture(unittest.WithBlock(&ms.UnfinalizedBlock))),
	)

	ms.receiptValidator.On("Validate", receipt).Return(nil)

	// setup the receipts mempool to check if we attempted to add the receipt to
	// the mempool
	ms.ReceiptsPL.On("Add", receipt).Return(true).Twice()

	// setup the results mempool to check if we attempted to insert the
	// incorporated result, and return false as if it was already in the mempool
	ms.ResultsPL.
		On("Add", incorporatedResult(receipt.ExecutionResult.BlockID, &receipt.ExecutionResult)).
		Return(false, nil).Twice()

	// onReceipt should return immediately after trying to pool the result, but
	// without throwing any errors
	err := ms.matching.onReceipt(receipt.ExecutorID, receipt)
	ms.Require().NoError(err, "should ignore receipt for already pending result")
	ms.ResultsPL.AssertNumberOfCalls(ms.T(), "Add", 1)
	ms.ResultsDB.AssertNumberOfCalls(ms.T(), "Store", 1)

	// resubmit receipt
	err = ms.matching.onReceipt(receipt.ExecutorID, receipt)
	ms.Require().NoError(err, "should ignore receipt for already pending result")
	ms.ReceiptsPL.AssertExpectations(ms.T())
	ms.ResultsPL.AssertExpectations(ms.T())
}

// try to submit a receipt that should be valid
func (ms *MatchingSuite) TestOnReceiptValid() {
	originID := ms.ExeID
	receipt := unittest.ExecutionReceiptFixture(
		unittest.WithExecutorID(originID),
		unittest.WithResult(unittest.ExecutionResultFixture(unittest.WithBlock(&ms.UnfinalizedBlock))),
	)

	ms.receiptValidator.On("Validate", receipt).Return(nil).Once()

	// we expect that receipt is added to mempool
	ms.ReceiptsPL.On("Add", receipt).Return(true).Once()

	// setup the results mempool to check if we attempted to add the incorporated result
	ms.ResultsPL.
		On("Add", incorporatedResult(receipt.ExecutionResult.BlockID, &receipt.ExecutionResult)).
		Return(true, nil).Once()

	// onReceipt should run to completion without throwing an error
	err := ms.matching.onReceipt(receipt.ExecutorID, receipt)
	ms.Require().NoError(err, "should add receipt and result to mempool if valid")

	ms.receiptValidator.AssertExpectations(ms.T())
	ms.ReceiptsPL.AssertExpectations(ms.T())
	ms.ResultsPL.AssertExpectations(ms.T())
}

// TestOnReceiptInvalid tests that we reject receipts that don't pass the ReceiptValidator
func (ms *MatchingSuite) TestOnReceiptInvalid() {
	// we use the same Receipt as in TestOnReceiptValid to ensure that the matching engine is not
	// rejecting the receipt for any other reason
	originID := ms.ExeID
	receipt := unittest.ExecutionReceiptFixture(
		unittest.WithExecutorID(originID),
		unittest.WithResult(unittest.ExecutionResultFixture(unittest.WithBlock(&ms.UnfinalizedBlock))),
	)
	ms.receiptValidator.On("Validate", receipt).Return(engine.NewInvalidInputError("")).Once()

	err := ms.matching.onReceipt(receipt.ExecutorID, receipt)
	ms.Require().Error(err, "should reject receipt that does not pass ReceiptValidator")
	ms.Assert().True(engine.IsInvalidInputError(err))

	ms.receiptValidator.AssertExpectations(ms.T())
	ms.ResultsDB.AssertNumberOfCalls(ms.T(), "Store", 0)
	ms.ResultsPL.AssertNumberOfCalls(ms.T(), "Add", 0)
}

// try to submit an approval where the message origin is inconsistent with the message creator
func (ms *MatchingSuite) TestApprovalInvalidOrigin() {
	// approval from valid origin (i.e. a verification node) but with random ApproverID
	originID := ms.VerID
	approval := unittest.ResultApprovalFixture() // with random ApproverID

	err := ms.matching.onApproval(originID, approval)
	ms.Require().Error(err, "should reject approval with mismatching origin and executor")
	ms.Require().True(engine.IsInvalidInputError(err))

	// approval from random origin but with valid ApproverID (i.e. a verification node)
	originID = unittest.IdentifierFixture() // random origin
	approval = unittest.ResultApprovalFixture(unittest.WithApproverID(ms.VerID))

	err = ms.matching.onApproval(originID, approval)
	ms.Require().Error(err, "should reject approval with mismatching origin and executor")
	ms.Require().True(engine.IsInvalidInputError(err))

	// In both cases, we expect the approval to be rejected without hitting the mempools
	ms.ApprovalsPL.AssertNumberOfCalls(ms.T(), "Add", 0)
}

// Try to submit an approval for an unknown block.
// As the block is unknown, the ID of the sender should
// not matter as there is no block to verify it against
func (ms *MatchingSuite) TestApprovalUnknownBlock() {
	originID := ms.ConID
	approval := unittest.ResultApprovalFixture(unittest.WithApproverID(originID)) // generates approval for random block ID

	// Make sure the approval is added to the cache for future processing
	ms.ApprovalsPL.On("Add", approval).Return(true, nil).Once()

	// onApproval should not throw an error
	err := ms.matching.onApproval(approval.Body.ApproverID, approval)
	ms.Require().NoError(err, "should cache approvals for unknown blocks")

	ms.ApprovalsPL.AssertExpectations(ms.T())
}

// try to submit an approval from a consensus node
func (ms *MatchingSuite) TestOnApprovalInvalidRole() {
	originID := ms.ConID
	approval := unittest.ResultApprovalFixture(
		unittest.WithBlockID(ms.UnfinalizedBlock.ID()),
		unittest.WithApproverID(originID),
	)

	err := ms.matching.onApproval(originID, approval)
	ms.Require().Error(err, "should reject approval from wrong approver role")
	ms.Require().True(engine.IsInvalidInputError(err))

	ms.ApprovalsPL.AssertNumberOfCalls(ms.T(), "Add", 0)
}

// try to submit an approval from an unstaked approver
func (ms *MatchingSuite) TestOnApprovalInvalidStake() {
	originID := ms.VerID
	approval := unittest.ResultApprovalFixture(
		unittest.WithBlockID(ms.UnfinalizedBlock.ID()),
		unittest.WithApproverID(originID),
	)
	ms.Identities[originID].Stake = 0

	err := ms.matching.onApproval(originID, approval)
	ms.Require().Error(err, "should reject approval from unstaked approver")
	ms.Require().True(engine.IsInvalidInputError(err))

	ms.ApprovalsPL.AssertNumberOfCalls(ms.T(), "Add", 0)
}

// try to submit an approval for a sealed result
func (ms *MatchingSuite) TestOnApprovalSealedResult() {
	originID := ms.VerID
	approval := unittest.ResultApprovalFixture(
		unittest.WithBlockID(ms.LatestSealedBlock.ID()),
		unittest.WithApproverID(originID),
	)

	err := ms.matching.onApproval(originID, approval)
	ms.Require().NoError(err, "should ignore approval for sealed result")

	ms.ApprovalsPL.AssertNumberOfCalls(ms.T(), "Add", 0)
}

// try to submit an approval that is already in the mempool
func (ms *MatchingSuite) TestOnApprovalPendingApproval() {
	originID := ms.VerID
	approval := unittest.ResultApprovalFixture(unittest.WithApproverID(originID))

	// setup the approvals mempool to check that we attempted to add the
	// approval, and return false as if it was already in the mempool
	ms.ApprovalsPL.On("Add", approval).Return(false, nil).Once()

	// onApproval should return immediately after trying to insert the approval,
	// without throwing any errors
	err := ms.matching.onApproval(approval.Body.ApproverID, approval)
	ms.Require().NoError(err, "should ignore approval if already pending")

	ms.ApprovalsPL.AssertExpectations(ms.T())
}

// try to submit an approval for a known block
func (ms *MatchingSuite) TestOnApprovalValid() {
	originID := ms.VerID
	approval := unittest.ResultApprovalFixture(
		unittest.WithBlockID(ms.UnfinalizedBlock.ID()),
		unittest.WithApproverID(originID),
	)

	// check that the approval is correctly added
	ms.ApprovalsPL.On("Add", approval).Return(true, nil).Once()

	// onApproval should run to completion without throwing any errors
	err := ms.matching.onApproval(approval.Body.ApproverID, approval)
	ms.Require().NoError(err, "should add approval to mempool if valid")

	ms.ApprovalsPL.AssertExpectations(ms.T())
}

// try to get matched results with nothing in memory pools
func (ms *MatchingSuite) TestSealableResultsEmptyMempools() {
	results, err := ms.matching.sealableResults()
	ms.Require().NoError(err, "should not error with empty mempools")
	ms.Assert().Empty(results, "should not have matched results with empty mempools")
}

// TestSealableResultsValid tests matching.Engine.sealableResults():
//  * a well-formed incorporated result R is in the mempool
//  * sufficient number of valid result approvals for result R
//  * R.PreviousResultID references a known result (i.e. stored in ResultsDB)
//  * R forms a valid sub-graph with its previous result (aka parent result)
// Method Engine.sealableResults() should return R as an element of the sealable results
func (ms *MatchingSuite) TestSealableResultsValid() {
	valSubgrph := ms.ValidSubgraphFixture()
	ms.AddSubgraphFixtureToMempools(valSubgrph)

	// test output of Matching Engine's sealableResults()
	results, err := ms.matching.sealableResults()
	ms.Require().NoError(err)
	ms.Assert().Equal(1, len(results), "expecting a single return value")
	ms.Assert().Equal(valSubgrph.IncorporatedResult.ID(), results[0].ID(), "expecting a single return value")
}

// Try to seal a result for which we don't have the block.
// This tests verifies that Matching engine is performing self-consistency checking:
// Not finding the block for an incorporated result is a fatal
// implementation bug, as we only add results to the IncorporatedResults
// mempool, where _both_ the block that incorporates the result as well
// as the block the result pertains to are known
func (ms *MatchingSuite) TestSealableResultsMissingBlock() {
	valSubgrph := ms.ValidSubgraphFixture()
	ms.AddSubgraphFixtureToMempools(valSubgrph)
	delete(ms.Blocks, valSubgrph.Block.ID()) // remove block the execution receipt pertains to

	_, err := ms.matching.sealableResults()
	ms.Require().Error(err)
}

// TestSealableResultsInvalidChunks tests that matching.Engine.sealableResults()
// performs the following chunk checks on the result:
//   * the number k of chunks in the execution result equals to
//     the number of collections in the corresponding block _plus_ 1 (for system chunk)
//   * for each index idx := 0, 1, ..., k
//     there exists once chunk
// Here we test that an IncorporatedResult with too _few_ chunks is not sealed and removed from the mempool
func (ms *MatchingSuite) TestSealableResults_TooFewChunks() {
	subgrph := ms.ValidSubgraphFixture()
	chunks := subgrph.Result.Chunks
	subgrph.Result.Chunks = chunks[0 : len(chunks)-2] // drop the last chunk
	ms.AddSubgraphFixtureToMempools(subgrph)

	// we expect business logic to remove the incorporated result with failed sub-graph check from mempool
	ms.ResultsPL.On("Rem", unittest.EntityWithID(subgrph.IncorporatedResult.ID())).Return(true).Once()

	results, err := ms.matching.sealableResults()
	ms.Require().NoError(err)
	ms.Assert().Empty(results, "should not select result with too many chunks")
	ms.ResultsPL.AssertExpectations(ms.T()) // asserts that ResultsPL.Rem(incorporatedResult.ID()) was called
}

// TestSealableResults_TooManyChunks tests that matching.Engine.sealableResults()
// performs the following chunk checks on the result:
//   * the number k of chunks in the execution result equals to
//     the number of collections in the corresponding block _plus_ 1 (for system chunk)
//   * for each index idx := 0, 1, ..., k
//     there exists once chunk
// Here we test that an IncorporatedResult with too _many_ chunks is not sealed and removed from the mempool
func (ms *MatchingSuite) TestSealableResults_TooManyChunks() {
	subgrph := ms.ValidSubgraphFixture()
	chunks := subgrph.Result.Chunks
	subgrph.Result.Chunks = append(chunks, chunks[len(chunks)-1]) // duplicate the last entry
	ms.AddSubgraphFixtureToMempools(subgrph)

	// we expect business logic to remove the incorporated result with failed sub-graph check from mempool
	ms.ResultsPL.On("Rem", unittest.EntityWithID(subgrph.IncorporatedResult.ID())).Return(true).Once()

	results, err := ms.matching.sealableResults()
	ms.Require().NoError(err)
	ms.Assert().Empty(results, "should not select result with too few chunks")
	ms.ResultsPL.AssertExpectations(ms.T()) // asserts that ResultsPL.Rem(incorporatedResult.ID()) was called
}

// TestSealableResults_InvalidChunks tests that matching.Engine.sealableResults()
// performs the following chunk checks on the result:
//   * the number k of chunks in the execution result equals to
//     the number of collections in the corresponding block _plus_ 1 (for system chunk)
//   * for each index idx := 0, 1, ..., k
//     there exists once chunk
// Here we test that an IncorporatedResult with
//   * correct number of chunks
//   * but one missing chunk and one duplicated chunk
// is not sealed and removed from the mempool
func (ms *MatchingSuite) TestSealableResults_InvalidChunks() {
	subgrph := ms.ValidSubgraphFixture()
	chunks := subgrph.Result.Chunks
	chunks[len(chunks)-2] = chunks[len(chunks)-1] // overwrite second-last with last entry, which is now duplicated
	// yet we have the correct number of elements in the chunk list
	ms.AddSubgraphFixtureToMempools(subgrph)

	// we expect business logic to remove the incorporated result with failed sub-graph check from mempool
	ms.ResultsPL.On("Rem", unittest.EntityWithID(subgrph.IncorporatedResult.ID())).Return(true).Once()

	results, err := ms.matching.sealableResults()
	ms.Require().NoError(err)
	ms.Assert().Empty(results, "should not select result with invalid chunk list")
	ms.ResultsPL.AssertExpectations(ms.T()) // asserts that ResultsPL.Rem(incorporatedResult.ID()) was called
}

// TestSealableResults_NoPayload_MissingChunk tests that matching.Engine.sealableResults()
// enforces the correct number of chunks for empty blocks, i.e. blocks with no payload:
//  * execution receipt with missing system chunk should be rejected
func (ms *MatchingSuite) TestSealableResults_NoPayload_MissingChunk() {
	subgrph := ms.ValidSubgraphFixture()
	subgrph.Block.Payload = nil                                                              // override block's payload to nil
	subgrph.IncorporatedResult.IncorporatedBlockID = subgrph.Block.ID()                      // update block's ID
	subgrph.IncorporatedResult.Result.BlockID = subgrph.Block.ID()                           // update block's ID
	subgrph.IncorporatedResult.Result.Chunks = subgrph.IncorporatedResult.Result.Chunks[0:0] // empty chunk list
	ms.AddSubgraphFixtureToMempools(subgrph)

	// we expect business logic to remove the incorporated result with failed sub-graph check from mempool
	ms.ResultsPL.On("Rem", unittest.EntityWithID(subgrph.IncorporatedResult.ID())).Return(true).Once()

	// the result should not be matched
	results, err := ms.matching.sealableResults()
	ms.Require().NoError(err)
	ms.Assert().Empty(results, "should not select result with invalid chunk list")
	ms.ResultsPL.AssertExpectations(ms.T()) // asserts that ResultsPL.Rem(incorporatedResult.ID()) was called
}

// TestSealableResults_NoPayload_TooManyChunk tests that matching.Engine.sealableResults()
// enforces the correct number of chunks for empty blocks, i.e. blocks with no payload:
//  * execution receipt with more than one chunk should be rejected
func (ms *MatchingSuite) TestSealableResults_NoPayload_TooManyChunk() {
	subgrph := ms.ValidSubgraphFixture()
	subgrph.Block.Payload = nil                                                              // override block's payload to nil
	subgrph.IncorporatedResult.IncorporatedBlockID = subgrph.Block.ID()                      // update block's ID
	subgrph.IncorporatedResult.Result.BlockID = subgrph.Block.ID()                           // update block's ID
	subgrph.IncorporatedResult.Result.Chunks = subgrph.IncorporatedResult.Result.Chunks[0:2] // two chunks
	ms.AddSubgraphFixtureToMempools(subgrph)

	// we expect business logic to remove the incorporated result with failed sub-graph check from mempool
	ms.ResultsPL.On("Rem", unittest.EntityWithID(subgrph.IncorporatedResult.ID())).Return(true).Once()

	results, err := ms.matching.sealableResults()
	ms.Require().NoError(err)
	ms.Assert().Empty(results, "should not select result with invalid chunk list")
	ms.ResultsPL.AssertExpectations(ms.T()) // asserts that ResultsPL.Rem(incorporatedResult.ID()) was called
}

// TestSealableResults_NoPayload_WrongIndexChunk tests that matching.Engine.sealableResults()
// enforces the correct number of chunks for empty blocks, i.e. blocks with no payload:
//  * execution receipt with a single chunk, but wrong chunk index, should be rejected
func (ms *MatchingSuite) TestSealableResults_NoPayload_WrongIndexChunk() {
	subgrph := ms.ValidSubgraphFixture()
	subgrph.Block.Payload = nil                                                              // override block's payload to nil
	subgrph.IncorporatedResult.IncorporatedBlockID = subgrph.Block.ID()                      // update block's ID
	subgrph.IncorporatedResult.Result.BlockID = subgrph.Block.ID()                           // update block's ID
	subgrph.IncorporatedResult.Result.Chunks = subgrph.IncorporatedResult.Result.Chunks[2:2] // chunk with chunkIndex == 2
	ms.AddSubgraphFixtureToMempools(subgrph)

	// we expect business logic to remove the incorporated result with failed sub-graph check from mempool
	ms.ResultsPL.On("Rem", unittest.EntityWithID(subgrph.IncorporatedResult.ID())).Return(true).Once()

	results, err := ms.matching.sealableResults()
	ms.Require().NoError(err)
	ms.Assert().Empty(results, "should not select result with invalid chunk list")
	ms.ResultsPL.AssertExpectations(ms.T()) // asserts that ResultsPL.Rem(incorporatedResult.ID()) was called
}

// TestSealableResultsUnassignedVerifiers tests that matching.Engine.sealableResults():
// only considers approvals from assigned verifiers
func (ms *MatchingSuite) TestSealableResultsUnassignedVerifiers() {
	subgrph := ms.ValidSubgraphFixture()

	assignedVerifiersPerChunk := uint(len(ms.Approvers) / 2)
	assignment := chunks.NewAssignment()
	approvals := make(map[uint64]map[flow.Identifier]*flow.ResultApproval)
	for _, chunk := range subgrph.IncorporatedResult.Result.Chunks {
		assignment.Add(chunk, ms.Approvers[0:assignedVerifiersPerChunk].NodeIDs()) // assign leading half verifiers

		// generate approvals by _tailing_ half verifiers
		chunkApprovals := make(map[flow.Identifier]*flow.ResultApproval)
		for _, approver := range ms.Approvers[assignedVerifiersPerChunk:len(ms.Approvers)] {
			chunkApprovals[approver.NodeID] = unittest.ApprovalFor(subgrph.IncorporatedResult.Result, chunk.Index, approver.NodeID)
		}
		approvals[chunk.Index] = chunkApprovals
	}
	subgrph.Assignment = assignment
	subgrph.Approvals = approvals

	ms.AddSubgraphFixtureToMempools(subgrph)

	results, err := ms.matching.sealableResults()
	ms.Require().NoError(err)
	ms.Assert().Empty(results, "should not select result with ")
	ms.ApprovalsPL.AssertExpectations(ms.T()) // asserts that ResultsPL.Rem(incorporatedResult.ID()) was called
}

// TestSealableResults_UnknownVerifiers tests that matching.Engine.sealableResults():
//   * removes approvals from unknown verification nodes from mempool
func (ms *MatchingSuite) TestSealableResults_ApprovalsForUnknownBlockRemain() {
	// make child block for UnfinalizedBlock, i.e.:
	//   <- UnfinalizedBlock <- block
	// and create Execution result ands approval for this block
	block := unittest.BlockWithParentFixture(ms.UnfinalizedBlock.Header)
	er := unittest.ExecutionResultFixture(unittest.WithBlock(&block))
	app1 := unittest.ApprovalFor(er, 0, unittest.IdentifierFixture()) // from unknown node

	ms.ApprovalsPL.On("All").Return([]*flow.ResultApproval{app1})
	chunkApprovals := make(map[flow.Identifier]*flow.ResultApproval)
	chunkApprovals[app1.Body.ApproverID] = app1
	ms.ApprovalsPL.On("ByChunk", er.ID(), 0).Return(chunkApprovals)

	_, err := ms.matching.sealableResults()
	ms.Require().NoError(err)
	ms.ApprovalsPL.AssertNumberOfCalls(ms.T(), "RemApproval", 0)
	ms.ApprovalsPL.AssertNumberOfCalls(ms.T(), "RemChunk", 0)
}

// TestRemoveApprovalsFromInvalidVerifiers tests that matching.Engine.sealableResults():
//   * removes approvals from invalid verification nodes from mempool
// This may occur when the block wasn't know when the node received the approval.
// Note: we test a scenario here, were result is sealable; it just has additional
//      approvals from invalid nodes
func (ms *MatchingSuite) TestRemoveApprovalsFromInvalidVerifiers() {
	subgrph := ms.ValidSubgraphFixture()

	// add invalid approvals to leading chunk:
	app1 := unittest.ApprovalFor(subgrph.IncorporatedResult.Result, 0, unittest.IdentifierFixture()) // from unknown node
	app2 := unittest.ApprovalFor(subgrph.IncorporatedResult.Result, 0, ms.ExeID)                     // from known but non-VerificationNode
	ms.Identities[ms.VerID].Stake = 0
	app3 := unittest.ApprovalFor(subgrph.IncorporatedResult.Result, 0, ms.VerID) // from zero-weight VerificationNode
	subgrph.Approvals[0][app1.Body.ApproverID] = app1
	subgrph.Approvals[0][app2.Body.ApproverID] = app2
	subgrph.Approvals[0][app3.Body.ApproverID] = app3

	ms.AddSubgraphFixtureToMempools(subgrph)

	// we expect business logic to remove the approval from the unknown node
	ms.ApprovalsPL.On("RemApproval", unittest.EntityWithID(app1.ID())).Return(true, nil).Once()
	ms.ApprovalsPL.On("RemApproval", unittest.EntityWithID(app2.ID())).Return(true, nil).Once()
	ms.ApprovalsPL.On("RemApproval", unittest.EntityWithID(app3.ID())).Return(true, nil).Once()

	_, err := ms.matching.sealableResults()
	ms.Require().NoError(err)
	ms.ApprovalsPL.AssertExpectations(ms.T()) // asserts that ResultsPL.Rem(incorporatedResult.ID()) was called
}

// TestSealableResultsInsufficientApprovals tests matching.Engine.sealableResults():
//  * a result where at least one chunk has not enough approvals (require
//    currently at least one) should not be sealable
func (ms *MatchingSuite) TestSealableResultsInsufficientApprovals() {
	subgrph := ms.ValidSubgraphFixture()
	delete(subgrph.Approvals, uint64(len(subgrph.Result.Chunks)-1))
	ms.AddSubgraphFixtureToMempools(subgrph)

	// test output of Matching Engine's sealableResults()
	results, err := ms.matching.sealableResults()
	ms.Require().NoError(err)
	ms.Assert().Empty(results, "expecting no sealable result")
}

// TestRequestPendingReceipts tests matching.Engine.requestPendingReceipts():
//   * generate n=100 consecutive blocks, where the first one is sealed and the last one is final
func (ms *MatchingSuite) TestRequestPendingReceipts() {
	// create blocks
	n := 100
	orderedBlocks := make([]flow.Block, 0, n)
	parentBlock := ms.UnfinalizedBlock
	for i := 0; i < n; i++ {
		block := unittest.BlockWithParentFixture(parentBlock.Header)
		ms.Blocks[block.ID()] = &block
		orderedBlocks = append(orderedBlocks, block)
		parentBlock = block
	}

	// progress latest sealed and latest finalized:
	ms.LatestSealedBlock = orderedBlocks[0]
	ms.LatestFinalizedBlock = orderedBlocks[n-1]

	// Expecting all blocks to be requested: from sealed height + 1 up to (incl.) latest finalized
	for i := 1; i < n; i++ {
		id := orderedBlocks[i].ID()
		ms.requester.On("EntityByID", id, mock.Anything).Return().Once()
	}
	ms.SealsPL.On("All").Return([]*flow.IncorporatedResultSeal{}).Maybe()

	err := ms.matching.requestPendingReceipts()
	ms.Require().NoError(err, "should request results for pending blocks")
	ms.requester.AssertExpectations(ms.T()) // asserts that requester.EntityByID(<blockID>, filter.Any) was called
}

// TestRequestPendingApprovals checks that requests are sent only for chunks
// that have not collected any approvals yet, and are sent only to the verifiers
// assigned to those chunks. It also check that the threshold and raite limiting
// is respected.
func (ms *MatchingSuite) TestRequestPendingApprovals() {

	// n is the total number of blocks and incorporated-results we add to the
	// chain and mempool
	n := 100

	// s is the number of incorporated results that have already collected at
	// least one approval per chunk, so they should not require any approval
	// requests
	s := 50

	// create blocks
	unsealedFinalizedBlocks := make([]flow.Block, 0, n)
	parentBlock := ms.UnfinalizedBlock
	for i := 0; i < n; i++ {
		block := unittest.BlockWithParentFixture(parentBlock.Header)
		ms.Blocks[block.ID()] = &block
		unsealedFinalizedBlocks = append(unsealedFinalizedBlocks, block)
		parentBlock = block
	}

	// progress latest sealed and latest finalized:
	ms.LatestSealedBlock = unsealedFinalizedBlocks[0]
	ms.LatestFinalizedBlock = unsealedFinalizedBlocks[n-1]

	// add an unfinalized block; it shouldn't require an approval request
	unfinalizedBlock := unittest.BlockWithParentFixture(parentBlock.Header)
	ms.Blocks[unfinalizedBlock.ID()] = &unfinalizedBlock

	// we will assume that all chunks are assigned to the same verifier. So each
	// approval request should only be sent to this single verifier.
	verifier := unittest.IdentifierFixture()

	// expectedRequests collects the set of ApprovalRequests that should be sent
	expectedRequests := []*messages.ApprovalRequest{}

	// populate the incorporated-results mempool with:
	// - 50 that have collected one signature per chunk
	// - 50 that have collected no signatures
	//
	// each chunk is assigned to the single verifier we defined above
	//
	// we populate expectedRequests with requests for chunks that have no
	// signatures, and for results that are for block that meet the approval
	// request threshold.
	//
	//
	//     sealed          unseale/finalized
	// |              ||                        |
	// 1 <- 2 <- .. <- s <- s+1 <- .. <- n-t <- n
	//                 |                  |
	//                    expected reqs
	for i := 0; i < n; i++ {

		// Create an incorporated result for unsealedFinalizedBlocks[i].
		// By default the result will contain 17 chunks.
		ir := unittest.IncorporatedResult.Fixture(
			unittest.IncorporatedResult.WithResult(
				unittest.ExecutionResultFixture(
					unittest.WithBlock(&unsealedFinalizedBlocks[i]),
				),
			),
			unittest.IncorporatedResult.WithIncorporatedBlockID(
				unsealedFinalizedBlocks[i].ID(),
			),
		)

		assignment := chunks.NewAssignment()

		for _, chunk := range ir.Result.Chunks {

			// assign the verifier to this chunk
			assignment.Add(chunk, flow.IdentifierList{verifier})
			ms.Assigner.On("Assign", ir.Result, ir.IncorporatedBlockID).Return(assignment, nil)

			// only add a signature if the result belongs to the set of results
			// that we assume have already received approvals for every chunk
			if i < s {
				ir.AddSignature(chunk.Index, unittest.IdentifierFixture(), unittest.SignatureFixture())
			} else {

				if i < n-int(ms.matching.approvalRequestsThreshold) {
					expectedRequests = append(expectedRequests,
						&messages.ApprovalRequest{
							ResultID:   ir.Result.ID(),
							ChunkIndex: chunk.Index,
						})
				}

			}
		}

		ms.PendingResults[ir.ID()] = ir
	}

	exp := n - s - int(ms.matching.approvalRequestsThreshold)

	// add an incorporated-result for a block that was already sealed. We
	// expect that no approval requests will be sent for this result, even if it
	// hasn't collected any approvals yet.
	sealedBlockIR := unittest.IncorporatedResult.Fixture(
		unittest.IncorporatedResult.WithResult(
			unittest.ExecutionResultFixture(
				unittest.WithBlock(&ms.LatestSealedBlock),
			),
		),
		unittest.IncorporatedResult.WithIncorporatedBlockID(
			ms.LatestSealedBlock.ID(),
		),
	)
	ms.PendingResults[sealedBlockIR.ID()] = sealedBlockIR

	// add an incorporated-result for an unfinalized block. It should not
	// generate any requests either.
	unfinalizedBlockIR := unittest.IncorporatedResult.Fixture(
		unittest.IncorporatedResult.WithResult(
			unittest.ExecutionResultFixture(
				unittest.WithBlock(&unfinalizedBlock),
			),
		),
		unittest.IncorporatedResult.WithIncorporatedBlockID(
			unfinalizedBlock.ID(),
		),
	)
	ms.PendingResults[unfinalizedBlock.ID()] = unfinalizedBlockIR

	// wire-up the approval requests conduit to keep track of all sent requests
	// and check that the target matches the unique verifier assigned to each
	// chunk
	requests := []*messages.ApprovalRequest{}
	conduit := &mocknetwork.Conduit{}
	conduit.On("Publish", mock.Anything, mock.Anything).
		Return(nil).
		Run(func(args mock.Arguments) {
			// collect the request
			ar, ok := args[0].(*messages.ApprovalRequest)
			ms.Assert().True(ok)
			requests = append(requests, ar)

			// check that the target is the verifier assigned to the chunk
			target, ok := args[1].(flow.Identifier)
			ms.Assert().True(ok)
			ms.Assert().Equal(verifier, target)
		})
	ms.matching.approvalConduit = conduit

	err := ms.matching.requestPendingApprovals()
	ms.Require().NoError(err)

	// first time it goes through, no requests should be made because of the
	// blackout period
	ms.Assert().Len(requests, 0)

	// Check the request tracker
	ms.Assert().Equal(exp, len(ms.matching.requestTracker.index))
	for _, expectedRequest := range expectedRequests {
		requestItem := ms.matching.requestTracker.Get(
			expectedRequest.ResultID,
			expectedRequest.ChunkIndex,
		)
		ms.Assert().Equal(uint(0), requestItem.Requests)
	}

	// wait for the max blackout period to elapse and retry
	time.Sleep(3 * time.Second)
	err = ms.matching.requestPendingApprovals()
	ms.Require().NoError(err)

	// now we expect that requests have been sent for the chunks that haven't
	// collected any approvals
	ms.Assert().Len(requests, len(expectedRequests))

	// Check the request tracker
	ms.Assert().Equal(exp, len(ms.matching.requestTracker.index))
	for _, expectedRequest := range expectedRequests {
		requestItem := ms.matching.requestTracker.Get(
			expectedRequest.ResultID,
			expectedRequest.ChunkIndex,
		)
		ms.Assert().Equal(uint(1), requestItem.Requests)
	}
}

// incorporatedResult returns a testify `argumentMatcher` that only accepts an
// IncorporatedResult with the given parameters
func incorporatedResult(blockID flow.Identifier, result *flow.ExecutionResult) interface{} {
	return mock.MatchedBy(func(ir *flow.IncorporatedResult) bool {
		return ir.IncorporatedBlockID == blockID && ir.Result.ID() == result.ID()
	})
}
