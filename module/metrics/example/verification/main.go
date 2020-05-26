package main

import (
	"math/rand"
	"time"

	"github.com/rs/zerolog"

	"github.com/dapperlabs/flow-go/model/verification"
	"github.com/dapperlabs/flow-go/module/mempool/stdmap"
	"github.com/dapperlabs/flow-go/module/metrics"
	"github.com/dapperlabs/flow-go/module/metrics/example"
	"github.com/dapperlabs/flow-go/module/trace"
	"github.com/dapperlabs/flow-go/utils/unittest"
)

// main runs a local tracer server on the machine and starts monitoring some metrics for sake of verification, which
// increases result approvals counter and checked chunks counter 100 times each
func main() {
	example.WithMetricsServer(func(logger zerolog.Logger) {
		tracer, err := trace.NewTracer(logger, "collection")
		if err != nil {
			panic(err)
		}

		verificationCollector := metrics.NewVerificationCollector(tracer)

		//authReceipts, err := stdmap.NewReceipts(10)
		//if err != nil {
		//	panic(err)
		//}
		//

		pendingReceipts, err := stdmap.NewPendingReceipts(10,
			stdmap.WithSizeMeterPendingReceipts(verificationCollector.OnPendingReceiptsUpdated))
		if err != nil {
			panic(err)
		}
		//
		//authCollections, err := stdmap.NewCollections(10)
		//if err != nil {
		//	panic(err)
		//}

		pendingCollections, err := stdmap.NewPendingCollections(10,
			stdmap.WithSizeMeterPendingCollections(verificationCollector.OnPendingCollectionsUpdated))
		if err != nil {
			panic(err)
		}

		//chunkDataPacks, err := stdmap.NewChunkDataPacks(10)
		//if err != nil {
		//	panic(err)
		//}
		//
		//chunkDataPackTrackers, err := stdmap.NewChunkDataPackTrackers(10)
		//if err != nil {
		//	panic(err)
		//}

		for i := 0; i < 100; i++ {
			chunkID := unittest.ChunkFixture().ID()
			verificationCollector.OnResultApproval()
			verificationCollector.OnChunkVerificationStarted(chunkID)

			// adds a collection to pending collections mempool
			coll := unittest.CollectionFixture(1)
			pendingCollections.Add(&verification.PendingCollection{
				Collection: &coll,
				OriginID:   unittest.IdentifierFixture(),
			})

			// adds a receipt to the pending receipts
			receipt := unittest.ExecutionReceiptFixture()
			pendingReceipts.Add(&verification.PendingReceipt{
				Receipt:  receipt,
				OriginID: unittest.IdentifierFixture(),
			})

			// adds a synthetic 1 s delay for verification duration
			time.Sleep(1 * time.Second)
			verificationCollector.OnChunkVerificationFinished(chunkID)
			verificationCollector.OnResultApproval()

			// storage tests
			// making randomized verifiable chunks that capture all storage per chunk
			verificationCollector.OnVerifiableChunkSubmitted(rand.Float64() * 10000.0)
		}
	})
}
