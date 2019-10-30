package types

// RegisterID (key part of key value)
type RegisterID [32]byte

// RegisterValue (value part of Register)
type RegisterValue [32]byte

type StorageProof []byte
type StateCommitment []byte

// Storage takes care of storing registers (key, value pairs) providing proof of correctness
// we aim to store a state of the order of 10^10 registers with up to 1M historic state versions
type Storage interface {
	// Trusted methods (without proof)
	// Get registers at specific StateCommitment by a list of register ids
	GetRegisters(registerIDs []RegisterID, stateCommitment StateCommitment) (registers []Register, err error)
	// Batched atomic updates of a subset of registers at specific state
	UpdateRegister(registerIDs []RegisterID, values []RegisterValue, stateCommitment StateCommitment) (newStateCommitment StateCommitment, err error)

	// Untrusted methods (providing proofs)
	// Get registers at specific StateCommitment by a list of register ids with proofs
	GetRegistersWithProof(registerIDs []RegisterID, stateCommitment StateCommitment) (registers []Register, proofs []StorageProof, err error)
	// Batched atomic updates of a subset of registers at specific state with proofs
	UpdateRegisterWithProof(registerIDs []RegisterID, values []RegisterValue, stateCommitment StateCommitment) (newStateCommitment StateCommitment, proofs []StorageProof, err error)
}

// StorageVerifier should be designed as an standalone package to verify proofs of storage
type StorageVerifier interface {
	// verify if a provided proof for getRegisters is accurate
	VerifyGetRegistersProof(registers []Register, proof StorageProof, stateCommitment StateCommitment) (verified bool, err error)
	// verify if a provided proof updateRegister is accurate
	VerifyUpdateRegistersProof(registers []Register, proof StorageProof, initialStateCommitment StateCommitment, finalStateCommitment StateCommitment) (verified bool, err error)
}
