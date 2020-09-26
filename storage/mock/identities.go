// Code generated by mockery v1.0.0. DO NOT EDIT.

package mock

import (
	flow "github.com/onflow/flow-go/model/flow"
	mock "github.com/stretchr/testify/mock"
)

// Identities is an autogenerated mock type for the Identities type
type Identities struct {
	mock.Mock
}

// ByNodeID provides a mock function with given fields: nodeID
func (_m *Identities) ByNodeID(nodeID flow.Identifier) (*flow.Identity, error) {
	ret := _m.Called(nodeID)

	var r0 *flow.Identity
	if rf, ok := ret.Get(0).(func(flow.Identifier) *flow.Identity); ok {
		r0 = rf(nodeID)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(*flow.Identity)
		}
	}

	var r1 error
	if rf, ok := ret.Get(1).(func(flow.Identifier) error); ok {
		r1 = rf(nodeID)
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}

// Store provides a mock function with given fields: identity
func (_m *Identities) Store(identity *flow.Identity) error {
	ret := _m.Called(identity)

	var r0 error
	if rf, ok := ret.Get(0).(func(*flow.Identity) error); ok {
		r0 = rf(identity)
	} else {
		r0 = ret.Error(0)
	}

	return r0
}
