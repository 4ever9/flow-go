package timeoutcollector

import (
	"fmt"
	"github.com/onflow/flow-go/consensus/hotstuff"
	"github.com/onflow/flow-go/consensus/hotstuff/model"
	"github.com/onflow/flow-go/model/flow"
	"go.uber.org/atomic"
	"sync"
)

// accumulatedWeightTracker tracks one-time event of reaching required weight
// Uses atomic flag to guarantee concurrency safety.
type accumulatedWeightTracker struct {
	minRequiredWeight uint64
	done              atomic.Bool
}

func (t *accumulatedWeightTracker) Done() bool {
	return t.done.Load()
}

// Track checks if required threshold was reached as one-time event and
// returns true whenever it's reached.
func (t *accumulatedWeightTracker) Track(weight uint64) bool {
	if weight < t.minRequiredWeight {
		return false
	}
	if t.done.CAS(false, true) {
		return true
	}
	return false
}

// highestQCTracker is a helper structure which keeps track of the highest QC(by view)
// in concurrency safe way.
type highestQCTracker struct {
	lock      sync.RWMutex
	highestQC *flow.QuorumCertificate
}

// Track updates local state of highestQC if the provided instance is higher(by view)
func (t *highestQCTracker) Track(qc *flow.QuorumCertificate) {
	highestQC := t.HighestQC()
	if highestQC != nil && highestQC.View >= qc.View {
		return
	}

	t.lock.Lock()
	defer t.lock.Unlock()
	if t.highestQC == nil || t.highestQC.View < qc.View {
		t.highestQC = qc
	}
}

func (t *highestQCTracker) HighestQC() *flow.QuorumCertificate {
	t.lock.RLock()
	defer t.lock.RUnlock()
	return t.highestQC
}

type TimeoutProcessor struct {
	view               uint64
	validator          hotstuff.Validator
	committee          hotstuff.Replicas
	sigAggregator      hotstuff.TimeoutSignatureAggregator
	onPartialTCCreated hotstuff.OnPartialTCCreated
	onTCCreated        hotstuff.OnTCCreated
	partialTCTracker   accumulatedWeightTracker
	tcTracker          accumulatedWeightTracker
	highestQCTracker   highestQCTracker
}

var _ hotstuff.TimeoutProcessor = (*TimeoutProcessor)(nil)

func NewTimeoutProcessor(committee hotstuff.Replicas,
	validator hotstuff.Validator,
	sigAggregator hotstuff.TimeoutSignatureAggregator,
	onPartialTCCreated hotstuff.OnPartialTCCreated,
	onTCCreated hotstuff.OnTCCreated,
) (*TimeoutProcessor, error) {
	view := sigAggregator.View()
	qcThreshold, err := committee.WeightThresholdForView(view)
	if err != nil {
		return nil, fmt.Errorf("could not retrieve weight threshold for view %d: %w", view, err)
	}
	return &TimeoutProcessor{
		view:      view,
		committee: committee,
		validator: validator,
		partialTCTracker: accumulatedWeightTracker{
			// TODO(active-pacemaker): fix this, add weight for f+1
			//minRequiredWeight: hotstuff.ComputeWeightThresholdForHonestMajority(totalWeight),
			minRequiredWeight: 1,
			done:              *atomic.NewBool(false),
		},
		tcTracker: accumulatedWeightTracker{
			minRequiredWeight: qcThreshold,
			done:              *atomic.NewBool(false),
		},
		onPartialTCCreated: onPartialTCCreated,
		onTCCreated:        onTCCreated,
		sigAggregator:      sigAggregator,
	}, nil
}

// Process performs processing of timeout object in concurrent safe way. This
// function is implemented to be called by multiple goroutines at the same time.
// Design of this function is event driven, as soon as we collect enough weight
// to create a TC or a partial TC we will immediately do this and submit it
// via callback for further processing.
// Expected error returns during normal operations:
// * hotstuff.TimeoutForIncompatibleViewError - submitted timeout for incompatible view
// * model.InvalidTimeoutError - submitted invalid timeout(invalid structure or invalid signature)
// All other errors should be treated as exceptions.
func (p *TimeoutProcessor) Process(timeout *model.TimeoutObject) error {
	if p.view != timeout.View {
		return fmt.Errorf("received incompatible timeout, expected %d got %d", p.view, timeout.View)
	}

	if p.tcTracker.Done() {
		return nil
	}

	err := p.validateTimeout(timeout)
	if err != nil {
		return fmt.Errorf("received invalid timeout: %w", err)
	}

	totalWeight, err := p.sigAggregator.VerifyAndAdd(timeout.SignerID, timeout.SigData, timeout.HighestQC.View)
	if err != nil {
		return fmt.Errorf("could not process invalid signature: %w", err)
	}

	p.highestQCTracker.Track(timeout.HighestQC)

	if p.partialTCTracker.Track(totalWeight) {
		p.onPartialTCCreated(p.view)
	}

	// checking of conditions for building TC are satisfied
	// At this point, we have enough signatures to build a TC. Another routine
	// might just be at this point. To avoid duplicate work, only one routine can pass:
	if !p.tcTracker.Track(totalWeight) {
		return nil
	}

	tc, err := p.buildTC()
	if err != nil {
		return fmt.Errorf("internal error constructing TC: %w", err)
	}
	p.onTCCreated(tc)

	return nil
}

// validateTimeout performs validation of timeout object, verifies if timeout is correctly structured
// and included QC and TC is correctly structured and signed.
// ATTENTION: this function doesn't check if timeout signature is valid, this check happens in signature aggregator
func (p *TimeoutProcessor) validateTimeout(timeout *model.TimeoutObject) error {
	// 1. check if it's correctly structured
	// (a) Every TO must contain a QC
	if timeout.HighestQC == nil {
		return model.NewInvalidTimeoutErrorf(timeout, "TimeoutObject without QC is invalid")
	}

	if timeout.View < timeout.HighestQC.View {
		return model.NewInvalidTimeoutErrorf(timeout, "TO's QC cannot be newer than the TO's view")
	}

	// (b) If a TC is included, the TC must be for the past round, no matter whether a QC
	//     for the last round is also included. In some edge cases, a node might observe
	//     _both_ QC and TC for the previous round, in which case it can include both.
	if timeout.LastViewTC != nil {
		if timeout.View != timeout.LastViewTC.View+1 {
			return model.NewInvalidTimeoutErrorf(timeout, "invalid TC for previous round")
		}
		if timeout.HighestQC.View < timeout.LastViewTC.TOHighestQC.View {
			return model.NewInvalidTimeoutErrorf(timeout, "timeout.HighestQC has older view that the QC in timeout.LastViewTC")
		}
	}
	// (c) The TO must contain a proof that sender legitimately entered timeout.View. Transitioning
	//     to round timeout.View is possible either by observing a QC or a TC for the previous round.
	//     If no QC is included, we require a TC to be present, which by check (1b) must be for
	//     the previous round.
	lastViewSuccessful := timeout.View == timeout.HighestQC.View+1
	if !lastViewSuccessful {
		// The TO's sender did _not_ observe a QC for round timeout.View-1. Hence, it should
		// include a TC for the previous round. Otherwise, the TO is invalid.
		if timeout.LastViewTC == nil {
			return model.NewInvalidTimeoutErrorf(timeout, "timeout must include TC")
		}
	}

	// 2. Check if QC is valid
	err := p.validator.ValidateQC(timeout.HighestQC)
	if err != nil {
		return model.NewInvalidTimeoutErrorf(timeout, "included QC is invalid: %w", err)
	}

	// 3. If TC is included, it must be valid
	if timeout.LastViewTC != nil {
		err = p.validator.ValidateTC(timeout.LastViewTC)
		if err != nil {
			return model.NewInvalidTimeoutErrorf(timeout, "included TC is invalid: %w", err)
		}
	}
	return nil

}

// buildTC performs aggregation of signatures when we have collected enough
// weight for building TC. This function is run only once by single worker.
// Any error should be treated as exception.
func (p *TimeoutProcessor) buildTC() (*flow.TimeoutCertificate, error) {
	signers, highQCViews, aggregatedSig, err := p.sigAggregator.Aggregate()
	if err != nil {
		return nil, fmt.Errorf("could not aggregate multi message signature: %w", err)
	}

	return &flow.TimeoutCertificate{
		View:          p.view,
		TOHighQCViews: highQCViews,
		TOHighestQC:   p.highestQCTracker.HighestQC(),
		SignerIDs:     signers,
		SigData:       aggregatedSig,
	}, nil
}
