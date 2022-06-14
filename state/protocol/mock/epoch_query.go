// Code generated by mockery v2.13.0. DO NOT EDIT.

package mock

import (
	protocol "github.com/onflow/flow-go/state/protocol"
	mock "github.com/stretchr/testify/mock"
)

// EpochQuery is an autogenerated mock type for the EpochQuery type
type EpochQuery struct {
	mock.Mock
}

// Current provides a mock function with given fields:
func (_m *EpochQuery) Current() protocol.Epoch {
	ret := _m.Called()

	var r0 protocol.Epoch
	if rf, ok := ret.Get(0).(func() protocol.Epoch); ok {
		r0 = rf()
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(protocol.Epoch)
		}
	}

	return r0
}

// Next provides a mock function with given fields:
func (_m *EpochQuery) Next() protocol.Epoch {
	ret := _m.Called()

	var r0 protocol.Epoch
	if rf, ok := ret.Get(0).(func() protocol.Epoch); ok {
		r0 = rf()
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(protocol.Epoch)
		}
	}

	return r0
}

// Previous provides a mock function with given fields:
func (_m *EpochQuery) Previous() protocol.Epoch {
	ret := _m.Called()

	var r0 protocol.Epoch
	if rf, ok := ret.Get(0).(func() protocol.Epoch); ok {
		r0 = rf()
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(protocol.Epoch)
		}
	}

	return r0
}

type NewEpochQueryT interface {
	mock.TestingT
	Cleanup(func())
}

// NewEpochQuery creates a new instance of EpochQuery. It also registers a testing interface on the mock and a cleanup function to assert the mocks expectations.
func NewEpochQuery(t NewEpochQueryT) *EpochQuery {
	mock := &EpochQuery{}
	mock.Mock.Test(t)

	t.Cleanup(func() { mock.AssertExpectations(t) })

	return mock
}
