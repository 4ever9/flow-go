package finder_test

import (
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	testifymock "github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/dapperlabs/flow-go/consensus/hotstuff/model"
	"github.com/dapperlabs/flow-go/engine/testutil"
	"github.com/dapperlabs/flow-go/engine/verification/utils"
	"github.com/dapperlabs/flow-go/model/flow"
	network "github.com/dapperlabs/flow-go/network/mock"
	"github.com/dapperlabs/flow-go/network/stub"
	"github.com/dapperlabs/flow-go/utils/unittest"
)

// testConcurrency evaluates behavior of verification node against:
// - ingest engine receives concurrent receipts from different sources
// - not all chunks of the receipts are assigned to the ingest engine
// - for each assigned chunk ingest engine emits a single result approval to verify engine only once
// (even in presence of duplication)
// - also the test stages to drop the first request on each collection to evaluate the retrial
// - also the test stages to drop the first request on each chunk data pack to evaluate the retrial
func TestConcurrency(t *testing.T) {
	var mu sync.Mutex
	testcases := []struct {
		erCount, // number of execution receipts
		senderCount, // number of (concurrent) senders for each execution receipt
		chunksNum int // number of chunks in each execution receipt
	}{
		{
			erCount:     1,
			senderCount: 1,
			chunksNum:   2,
		},
		{
			erCount:     1,
			senderCount: 5,
			chunksNum:   2,
		},
		{
			erCount:     5,
			senderCount: 1,
			chunksNum:   2,
		},
		{
			erCount:     5,
			senderCount: 5,
			chunksNum:   2,
		},
		{
			erCount:     1,
			senderCount: 1,
			chunksNum:   10, // choosing a higher number makes the test longer and longer timeout needed
		},
		{
			erCount:     2,
			senderCount: 5,
			chunksNum:   4,
		},
		{
			erCount:     1,
			senderCount: 1,
			chunksNum:   2,
		},
	}

	for _, tc := range testcases {
		t.Run(fmt.Sprintf("%d-ers/%d-senders/%d-chunks",
			tc.erCount, tc.senderCount, tc.chunksNum), func(t *testing.T) {
			mu.Lock()
			defer mu.Unlock()
			testConcurrency(t, tc.erCount, tc.senderCount, tc.chunksNum)
		})
	}
}

func testConcurrency(t *testing.T, erCount, senderCount, chunksNum int) {
	log := zerolog.New(os.Stderr).Level(zerolog.DebugLevel)
	// to demarcate the logs
	log.Debug().
		Int("execution_receipt_count", erCount).
		Int("sender_count", senderCount).
		Int("chunks_num", chunksNum).
		Msg("TestConcurrency started")
	hub := stub.NewNetworkHub()

	// creates test id for each role
	colID := unittest.IdentityFixture(unittest.WithRole(flow.RoleCollection))
	conID := unittest.IdentityFixture(unittest.WithRole(flow.RoleConsensus))
	exeID := unittest.IdentityFixture(unittest.WithRole(flow.RoleExecution))
	verID := unittest.IdentityFixture(unittest.WithRole(flow.RoleVerification))

	identities := flow.IdentityList{colID, conID, exeID, verID}

	// create `erCount` ER fixtures that will be concurrently delivered
	ers := make([]utils.CompleteExecutionResult, 0)
	results := make([]flow.ExecutionResult, len(ers))

	for i := 0; i < erCount; i++ {
		er := utils.LightExecutionResultFixture(chunksNum)
		ers = append(ers, er)
		results = append(results, er.Receipt.ExecutionResult)
	}

	// set up mock maching engine that asserts each receipt is submitted exactly once.
	requestInterval := uint(1000)
	failureThreshold := uint(2)
	matchEng, matchEngWG := SetupMockMatchEng(t, results)
	assigner := utils.NewMockAssigner(verID.NodeID, utils.IsAssigned)
	verNode := testutil.VerificationNode(t, hub, verID, identities,
		assigner, requestInterval, failureThreshold, true,
		true, testutil.WithMatchEngine(matchEng))

	// the wait group tracks goroutines for each Execution Receipt sent to Finder engine
	var senderWG sync.WaitGroup
	senderWG.Add(erCount * senderCount)

	var blockStorageLock sync.Mutex

	for _, completeER := range ers {

		// spin up `senderCount` sender goroutines to mimic receiving
		// the same resource multiple times
		for i := 0; i < senderCount; i++ {
			go func(j int, id flow.Identifier, block *flow.Block, receipt *flow.ExecutionReceipt) {

				// sendBlock makes the block associated with the receipt available to the
				// follower engine of the verification node
				sendBlock := func() {
					// adds the block to the storage of the node
					// Note: this is done by the follower
					// this block should be done in a thread-safe way
					blockStorageLock.Lock()
					// we don't check for error as it definitely returns error when we
					// have duplicate blocks, however, this is not the concern for this test
					_ = verNode.Blocks.Store(block)
					blockStorageLock.Unlock()

					// casts block into a Hotstuff block for notifier
					hotstuffBlock := &model.Block{
						BlockID:     block.ID(),
						View:        block.Header.View,
						ProposerID:  block.Header.ProposerID,
						QC:          nil,
						PayloadHash: block.Header.PayloadHash,
						Timestamp:   block.Header.Timestamp,
					}
					verNode.FinderEngine.OnFinalizedBlock(hotstuffBlock)
				}

				// sendReceipt sends the execution receipt to the finder engine of verification node
				sendReceipt := func() {
					err := verNode.FinderEngine.Process(exeID.NodeID, receipt)
					require.NoError(t, err)
				}

				switch j % 2 {
				case 0:
					// block then receipt
					sendBlock()
					// allow another goroutine to run before sending receipt
					time.Sleep(time.Nanosecond)
					sendReceipt()
				case 1:
					// receipt then block
					sendReceipt()
					// allow another goroutine to run before sending block
					time.Sleep(time.Nanosecond)
					sendBlock()
				}

				senderWG.Done()
			}(i, completeER.Receipt.ExecutionResult.ID(), completeER.Block, completeER.Receipt)
		}
	}

	// waits for all receipts to be sent to verification node
	unittest.RequireReturnsBefore(t, senderWG.Wait, time.Duration(senderCount*chunksNum*erCount*5)*time.Second)
	// waits for all distinct execution results sent to matching engine of verification node
	unittest.RequireReturnsBefore(t, matchEngWG.Wait, time.Duration(senderCount*chunksNum*erCount*5)*time.Second)

	for _, er := range ers {
		// all distinct execution results should be processed
		assert.True(t, verNode.IngestedResultIDs.Has(er.Receipt.ExecutionResult.ID()))
	}

	verNode.Done()

	// to demarcate the logs
	log.Debug().
		Int("execution_receipt_count", erCount).
		Int("sender_count", senderCount).
		Int("chunks_num", chunksNum).
		Msg("TestConcurrency finished")
}

// SetupMockMatchEng sets up a mock match engine that asserts the followings:
// - that a set execution results are delivered to it.
// - that each execution result is delivered only once.
// SetupMockMatchEng returns the mock engine and a wait group that unblocks when all results are received.
func SetupMockMatchEng(t testing.TB, ers []flow.ExecutionResult) (*network.Engine, *sync.WaitGroup) {
	eng := new(network.Engine)

	// keep track of which verifiable chunks we have received
	receivedERs := make(map[flow.Identifier]struct{})
	var (
		// decrement the wait group when each verifiable chunk received
		wg sync.WaitGroup
		// check one verifiable chunk at a time to ensure dupe checking works
		mu sync.Mutex
	)

	// expects `len(er)` many execution results
	wg.Add(len(ers))

	eng.On("ProcessLocal", testifymock.Anything).
		Run(func(args testifymock.Arguments) {
			mu.Lock()
			defer mu.Unlock()

			// the received entity should be an execution result
			result, ok := args[0].(*flow.ExecutionResult)
			assert.True(t, ok)

			resultID := result.ID()

			// verifies that it has not seen this result
			_, alreadySeen := receivedERs[resultID]
			if alreadySeen {
				t.Logf("match engine received duplicate ER (id=%s)", result.ID())
				t.Fail()
				return
			}

			// ensure the received result matches one we expect
			for _, er := range ers {
				if resultID == er.ID() {
					// mark it as seen and decrement the waitgroup
					receivedERs[resultID] = struct{}{}
					wg.Done()
					return
				}
			}

			// the received result doesn't match any expected result
			t.Logf("received unexpected results (id=%s)", resultID)
			t.Fail()
		}).
		Return(nil)

	return eng, &wg
}
