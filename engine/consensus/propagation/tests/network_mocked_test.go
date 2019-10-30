package tests

import (
	"io/ioutil"
	"math/rand"
	"testing"

	"github.com/pkg/errors"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/dapperlabs/flow-go/engine/consensus/propagation"
	"github.com/dapperlabs/flow-go/engine/consensus/propagation/mempool"
	"github.com/dapperlabs/flow-go/engine/consensus/propagation/volatile"
	"github.com/dapperlabs/flow-go/model/collection"
	"github.com/dapperlabs/flow-go/module/committee"
	"github.com/dapperlabs/flow-go/network/mock"
)

// mockPropagationNode is a mocked node instance for testing propagation engine.
type mockPropagationNode struct {
	// the real engine to be tested
	engine *propagation.Engine
	// a mocked network layer in order for the Hub to route events in memory to a targeted node
	net *mock.Network
	// the state of the engine, exposed in order for tests to assert
	pool *mempool.Mempool
}

// newMockPropagationNode creates a mocked node with a real engine in it, and "plug" the node into a mocked hub.
func newMockPropagationNode(hub *mock.Hub, allNodes []string, nodeIndex int) (*mockPropagationNode, error) {
	if nodeIndex >= len(allNodes) {
		return nil, errors.Errorf("nodeIndex is out of range: %v", nodeIndex)
	}

	nodeEntry := allNodes[nodeIndex]

	nodeID, err := committee.EntryToID(nodeEntry)
	if err != nil {
		return nil, err
	}

	log := zerolog.New(ioutil.Discard)

	pool, err := mempool.New()
	if err != nil {
		return nil, err
	}

	com, err := committee.New(allNodes, nodeID)
	if err != nil {
		return nil, err
	}

	net, err := mock.NewNetwork(com, hub)
	if err != nil {
		return nil, err
	}

	vol, err := volatile.New()
	if err != nil {
		return nil, err
	}

	engine, err := propagation.New(log, net, com, pool, vol)
	if err != nil {
		return nil, err
	}

	return &mockPropagationNode{
		engine: engine,
		net:    net,
		pool:   pool,
	}, nil
}

func createConnectedNodes(nodeEntries []string) (*mock.Hub, []*mockPropagationNode, error) {
	if len(nodeEntries) == 0 {
		return nil, nil, errors.New("NodeEntries must not be empty")
	}

	hub := mock.NewNetworkHub()

	nodes := make([]*mockPropagationNode, 0)
	for i := range nodeEntries {
		node, err := newMockPropagationNode(hub, nodeEntries, i)
		if err != nil {
			return nil, nil, err
		}
		nodes = append(nodes, node)
	}

	return hub, nodes, nil
}

// a utility func to return a random collection hash
func randHash() ([]byte, error) {
	hash := make([]byte, 32)
	_, err := rand.Read(hash)
	return hash, err
}

func TestSubmitCollection(t *testing.T) {
	// If a consensus node receives a collection hash, then another connected node should receive it as well.
	t.Run("should propagate collection to connected nodes", func(t *testing.T) {
		// create a mocked network for each node and connect them in a in-memory hub, so that events sent from one engine
		// can be delivery directly to another engine on a different node
		_, nodes, err := createConnectedNodes([]string{"consensus-consensus1@localhost:7297", "consensus-consensus2@localhost:7297"})
		require.Nil(t, err)

		node1 := nodes[0]
		node2 := nodes[1]

		hash, err := randHash()
		require.Nil(t, err)

		gc := &collection.GuaranteedCollection{
			Hash: hash,
		}
		// node1's engine receives a collection hash
		err = node1.engine.Process(node1.net.GetID(), gc)
		require.Nil(t, err)

		// inspect node2's mempool to check if node2's engine received the collection hash
		coll, err := node2.pool.Get(hash)
		require.Nil(t, err)

		// should match
		require.Equal(t, coll.Hash, hash)
	})
}
