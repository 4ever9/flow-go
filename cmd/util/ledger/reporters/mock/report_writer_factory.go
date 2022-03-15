// Code generated by mockery v1.0.0. DO NOT EDIT.

package mock

import (
	reporters "github.com/onflow/flow-go/cmd/util/ledger/reporters"
	mock "github.com/stretchr/testify/mock"
)

// ReportWriterFactory is an autogenerated mock type for the ReportWriterFactory type
type ReportWriterFactory struct {
	mock.Mock
}

// ReportWriter provides a mock function with given fields: dataNamespace
func (_m *ReportWriterFactory) ReportWriter(dataNamespace string) reporters.ReportWriter {
	ret := _m.Called(dataNamespace)

	var r0 reporters.ReportWriter
	if rf, ok := ret.Get(0).(func(string) reporters.ReportWriter); ok {
		r0 = rf(dataNamespace)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(reporters.ReportWriter)
		}
	}

	return r0
}
