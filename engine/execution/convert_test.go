package execution_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/onflow/flow-go/engine/execution"
	executionUnittest "github.com/onflow/flow-go/engine/execution/state/unittest"
	"github.com/onflow/flow-go/model/flow"
	modulemock "github.com/onflow/flow-go/module/mock"
	"github.com/onflow/flow-go/utils/unittest"
)

func Test_BuildChunkDataPack(t *testing.T) {
	t.Run("number of transactions is included", func(t *testing.T) {

		// fixture provide one tx per collection, and number of collections equal to
		// len of provided signersIDs
		cr := executionUnittest.ComputationResultFixture([][]flow.Identifier{
			{flow.ZeroID},
			{flow.ZeroID},
			{flow.ZeroID},
		})

		numberOfChunks := 4
		exemetrics := new(modulemock.ExecutionMetrics)
		exemetrics.On("ExecutionChunkDataPackGenerated",
			mock.Anything,
			mock.Anything).
			Return(nil).
			Times(numberOfChunks) // 1 collection + system collection

		_, _, result, err := execution.GenerateExecutionResultAndChunkDataPacks(exemetrics, unittest.IdentifierFixture(), unittest.StateCommitmentFixture(), cr)
		assert.NoError(t, err)

		require.Len(t, result.Chunks, numberOfChunks) // +1 for system chunk

		assert.Equal(t, uint64(1), result.Chunks[0].NumberOfTransactions)
		assert.Equal(t, uint64(1), result.Chunks[1].NumberOfTransactions)
		assert.Equal(t, uint64(1), result.Chunks[2].NumberOfTransactions)

		// system chunk is special case, but currently also 1 tx
		assert.Equal(t, uint64(1), result.Chunks[3].NumberOfTransactions)
	})
}
