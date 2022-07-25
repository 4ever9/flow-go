package tracker

import (
	"unsafe"

	"go.uber.org/atomic"

	"github.com/onflow/flow-go/model/flow"
)

// NewestQCTracker is a helper structure which keeps track of the highest QC(by view)
// in concurrency safe way.
type NewestQCTracker struct {
	newestQC *atomic.UnsafePointer
}

func NewNewestQCTracker() *NewestQCTracker {
	tracker := &NewestQCTracker{
		newestQC: atomic.NewUnsafePointer(unsafe.Pointer(nil)),
	}
	return tracker
}

// Track updates local state of NewestQC if the provided instance is newer(by view)
// Concurrently safe
func (t *NewestQCTracker) Track(qc *flow.QuorumCertificate) bool {
	// to record the newest value that we have ever seen we need to use loop
	// with CAS atomic operation to make sure that we always write the latest value
	// in case of shared access to updated value.
	for {
		// take a snapshot
		NewestQC := t.NewestQC()
		// verify that our update makes sense
		if NewestQC != nil && NewestQC.View >= qc.View {
			return false
		}
		// attempt to install new value, repeat in case of shared update.
		if t.newestQC.CAS(unsafe.Pointer(NewestQC), unsafe.Pointer(qc)) {
			return true
		}
	}
}

// NewestQC returns the newest QC(by view) tracked.
// Concurrently safe
func (t *NewestQCTracker) NewestQC() *flow.QuorumCertificate {
	return (*flow.QuorumCertificate)(t.newestQC.Load())
}
