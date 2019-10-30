// Package constants holds constant values defined for the Flow protocol.
package constants

const (
	// AccountKeyWeightThreshold is the total weight required for a set of keys to unlock an account.
	AccountKeyWeightThreshold int = 1000
)

// List of account event types.
const (
	EventAccountCreated string = "flow.AccountCreated"
	EventAccountUpdated string = "flow.AccountUpdated"
)
