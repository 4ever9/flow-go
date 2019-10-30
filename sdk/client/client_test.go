package client_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"

	"github.com/dapperlabs/flow-go/model/flow"
	"github.com/dapperlabs/flow-go/proto/services/observation"
	"github.com/dapperlabs/flow-go/sdk/client"
	"github.com/dapperlabs/flow-go/sdk/client/mocks"
	"github.com/dapperlabs/flow-go/sdk/convert"
	"github.com/dapperlabs/flow-go/utils/unittest"
)

func TestSendTransaction(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	mockRPC := mocks.NewMockRPCClient(mockCtrl)

	c := client.NewFromRPCClient(mockRPC)
	ctx := context.Background()

	tx := unittest.TransactionFixture()

	t.Run("Success", func(t *testing.T) {
		// client should return non-error if RPC call succeeds
		mockRPC.EXPECT().
			SendTransaction(ctx, gomock.Any()).
			Return(&observation.SendTransactionResponse{Hash: tx.Hash()}, nil).
			Times(1)

		err := c.SendTransaction(ctx, tx)
		assert.Nil(t, err)
	})

	t.Run("Server error", func(t *testing.T) {
		// client should return error if RPC call fails
		mockRPC.EXPECT().
			SendTransaction(ctx, gomock.Any()).
			Return(nil, errors.New("dummy error")).
			Times(1)

		// error should be passed to user
		err := c.SendTransaction(ctx, tx)
		assert.Error(t, err)
	})
}

func TestGetLatestBlock(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	mockRPC := mocks.NewMockRPCClient(mockCtrl)

	c := client.NewFromRPCClient(mockRPC)
	ctx := context.Background()

	res := &observation.GetLatestBlockResponse{
		Block: convert.BlockHeaderToMessage(unittest.BlockHeaderFixture()),
	}

	t.Run("Success", func(t *testing.T) {
		// client should return non-error if RPC call succeeds
		mockRPC.EXPECT().
			GetLatestBlock(ctx, gomock.Any()).
			Return(res, nil).
			Times(1)

		blockHeaderA, err := c.GetLatestBlock(ctx, true)
		assert.Nil(t, err)

		blockHeaderB := convert.MessageToBlockHeader(res.GetBlock())
		assert.Equal(t, *blockHeaderA, blockHeaderB)
	})

	t.Run("Server error", func(t *testing.T) {
		// client should return error if RPC call fails
		mockRPC.EXPECT().
			GetLatestBlock(ctx, gomock.Any()).
			Return(nil, errors.New("dummy error")).
			Times(1)

		_, err := c.GetLatestBlock(ctx, true)
		assert.Error(t, err)
	})
}

func TestExecuteScript(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	mockRPC := mocks.NewMockRPCClient(mockCtrl)

	c := client.NewFromRPCClient(mockRPC)
	ctx := context.Background()

	valueBytes, _ := json.Marshal(1)

	t.Run("Success", func(t *testing.T) {
		// client should return non-error if RPC call succeeds
		mockRPC.EXPECT().
			ExecuteScript(ctx, gomock.Any()).
			Return(&observation.ExecuteScriptResponse{Value: valueBytes}, nil).
			Times(1)

		value, err := c.ExecuteScript(ctx, []byte("fun main(): Int { return 1 }"))
		assert.Nil(t, err)
		assert.Equal(t, value, float64(1))
	})

	t.Run("Server error", func(t *testing.T) {
		// client should return error if RPC call fails
		mockRPC.EXPECT().
			ExecuteScript(ctx, gomock.Any()).
			Return(nil, errors.New("dummy error")).
			Times(1)

		// error should be passed to user
		_, err := c.ExecuteScript(ctx, []byte("fun main(): Int { return 1 }"))
		assert.Error(t, err)
	})

	t.Run("Error - empty return value", func(t *testing.T) {
		// client should return error if value is empty
		mockRPC.EXPECT().
			ExecuteScript(ctx, gomock.Any()).
			Return(&observation.ExecuteScriptResponse{Value: []byte{}}, nil).
			Times(1)

		// error should be passed to user
		_, err := c.ExecuteScript(ctx, []byte("fun main(): Int { return 1 }"))
		assert.Error(t, err)
	})

	t.Run("Error - malformed return value", func(t *testing.T) {
		// client should return error if value is malformed
		mockRPC.EXPECT().
			ExecuteScript(ctx, gomock.Any()).
			Return(&observation.ExecuteScriptResponse{Value: []byte("asdfafa")}, nil).
			Times(1)

		// error should be passed to user
		_, err := c.ExecuteScript(ctx, []byte("fun main(): Int { return 1 }"))
		assert.Error(t, err)
	})
}

func TestGetEvents(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	mockRPC := mocks.NewMockRPCClient(mockCtrl)

	c := client.NewFromRPCClient(mockRPC)
	ctx := context.Background()

	// Set up a mock event response
	mockEvent := flow.Event{
		ID: "Transfer",
		Values: map[string]interface{}{
			"to":   flow.ZeroAddress,
			"from": flow.ZeroAddress,
			"id":   1,
		},
	}
	events := []*flow.Event{&mockEvent}

	var buf bytes.Buffer
	err := json.NewEncoder(&buf).Encode(events)
	assert.Nil(t, err)

	t.Run("Success", func(t *testing.T) {
		// Set up the mock to return a mocked event response
		mockRPC.EXPECT().
			GetEvents(ctx, gomock.Any()).
			Return(&observation.GetEventsResponse{EventsJson: buf.Bytes()}, nil).
			Times(1)

		// The client should pass the response to the client
		res, err := c.GetEvents(ctx, &flow.EventQuery{})
		assert.Nil(t, err)
		assert.Equal(t, len(res), 1)
		assert.Equal(t, res[0].ID, mockEvent.ID)
	})

	t.Run("Server error", func(t *testing.T) {
		// Set up the mock to return an error
		mockRPC.EXPECT().
			GetEvents(ctx, gomock.Any()).
			Return(nil, fmt.Errorf("dummy error")).
			Times(1)

		// The client should pass along the error
		_, err = c.GetEvents(ctx, &flow.EventQuery{})
		assert.Error(t, err)
	})

	t.Run("Error - malformed return value", func(t *testing.T) {
		// Set up the mock to return a malformed eventsJSON response
		mockRPC.EXPECT().
			GetEvents(ctx, gomock.Any()).
			Return(&observation.GetEventsResponse{EventsJson: []byte{1, 2, 3, 4}}, nil).
			Times(1)

		// The client should return an error because it should fail to decode
		_, err = c.GetEvents(ctx, &flow.EventQuery{})
		assert.Error(t, err)
	})
}
