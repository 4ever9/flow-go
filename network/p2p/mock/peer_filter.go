// Code generated by mockery v2.13.1. DO NOT EDIT.

package mockp2p

import (
	mock "github.com/stretchr/testify/mock"

	peer "github.com/libp2p/go-libp2p/core/peer"
)

// PeerFilter is an autogenerated mock type for the PeerFilter type
type PeerFilter struct {
	mock.Mock
}

// Execute provides a mock function with given fields: _a0
func (_m *PeerFilter) Execute(_a0 peer.ID) error {
	ret := _m.Called(_a0)

	var r0 error
	if rf, ok := ret.Get(0).(func(peer.ID) error); ok {
		r0 = rf(_a0)
	} else {
		r0 = ret.Error(0)
	}

	return r0
}

type mockConstructorTestingTNewPeerFilter interface {
	mock.TestingT
	Cleanup(func())
}

// NewPeerFilter creates a new instance of PeerFilter. It also registers a testing interface on the mock and a cleanup function to assert the mocks expectations.
func NewPeerFilter(t mockConstructorTestingTNewPeerFilter) *PeerFilter {
	mock := &PeerFilter{}
	mock.Mock.Test(t)

	t.Cleanup(func() { mock.AssertExpectations(t) })

	return mock
}
