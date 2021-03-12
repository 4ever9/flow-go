// Code generated by mockery v1.0.0. DO NOT EDIT.

package mock

import mock "github.com/stretchr/testify/mock"

// BatchStorage is an autogenerated mock type for the BatchStorage type
type BatchStorage struct {
	mock.Mock
}

// Set provides a mock function with given fields: key, val
func (_m *BatchStorage) Set(key []byte, val []byte) error {
	ret := _m.Called(key, val)

	var r0 error
	if rf, ok := ret.Get(0).(func([]byte, []byte) error); ok {
		r0 = rf(key, val)
	} else {
		r0 = ret.Error(0)
	}

	return r0
}
