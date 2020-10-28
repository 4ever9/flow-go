// Code generated by mockery v1.0.0. DO NOT EDIT.

package mock

import (
	flow "github.com/onflow/flow-go/model/flow"
	mock "github.com/stretchr/testify/mock"

	time "time"
)

// ConsensusMetrics is an autogenerated mock type for the ConsensusMetrics type
type ConsensusMetrics struct {
	mock.Mock
}

// CheckSealingDuration provides a mock function with given fields: duration
func (_m *ConsensusMetrics) CheckSealingDuration(duration time.Duration) {
	_m.Called(duration)
}

// FinishBlockToSeal provides a mock function with given fields: blockID
func (_m *ConsensusMetrics) FinishBlockToSeal(blockID flow.Identifier) {
	_m.Called(blockID)
}

// FinishCollectionToFinalized provides a mock function with given fields: collectionID
func (_m *ConsensusMetrics) FinishCollectionToFinalized(collectionID flow.Identifier) {
	_m.Called(collectionID)
}

// StartBlockToSeal provides a mock function with given fields: blockID
func (_m *ConsensusMetrics) StartBlockToSeal(blockID flow.Identifier) {
	_m.Called(blockID)
}

// StartCollectionToFinalized provides a mock function with given fields: collectionID
func (_m *ConsensusMetrics) StartCollectionToFinalized(collectionID flow.Identifier) {
	_m.Called(collectionID)
}
