package crypto

import (
	"errors"
	"fmt"
	bn256 "github.com/onflow/flow-go/crypto/bn254/cloudflare"
	"github.com/onflow/flow-go/crypto/hash"
	"math/big"
)

// blsAggregateEmptyListError is returned when a list of BLS objects (e.g. signatures or keys)
// is empty or nil and thereby represents an invalid input.
var bn256AggregateEmptyListError = errors.New("list cannot be empty")

// IsBN256AggregateEmptyListError checks if err is an `blsAggregateEmptyListError`.
// blsAggregateEmptyListError is returned when a BLS aggregation function is called with
// an empty list which is not allowed in some aggregation cases to avoid signature equivocation
// issues.
func IsBN256AggregateEmptyListError(err error) bool {
	return errors.Is(err, bn256AggregateEmptyListError)
}

// AggregateBN256PrivateKeys aggregates multiple BLS private keys into one.
//
// The order of the keys in the slice does not matter since the aggregation
// is commutative. The slice should not be empty.
// No check is performed on the input private keys.
// Input or output private keys could be equal to the identity element (zero). Note that any
// signature generated by the identity key is invalid (to avoid equivocation issues).
//
// The function returns:
//   - (nil, notBLSKeyError) if at least one key is not of type BLS BLS12-381
//   - (nil, blsAggregateEmptyListError) if no keys are provided (input slice is empty)
//   - (aggregated_key, nil) otherwise
func AggregateBN256PrivateKeys(keys []PrivateKey) (PrivateKey, error) {
	if len(keys) == 0 {
		return nil, bn256AggregateEmptyListError
	}

	points := make([]*bn256.G1, len(keys))
	sp := new(big.Int)
	for i := 0; i < len(keys); i++ {
		sk, ok := keys[i].(*prKeyBLSBN256)
		if !ok {
			return nil, fmt.Errorf("wrong BN256 private key")
		}

		points[i] = sk.point
		sp.Add(sp, sk.s)
	}
	p := aggregatePointG1(points)

	return &prKeyBLSBN256{
		s:     sp,
		point: p,
	}, nil
}

func AggregateBN256Signatures(sigs []Signature) (Signature, error) {
	// check for empty list
	if len(sigs) == 0 {
		return nil, bn256AggregateEmptyListError
	}

	points := make([]*bn256.G1, len(sigs))
	for i := 0; i < len(sigs); i++ {
		point, err := GetBN256SignaturePoint(sigs[i])
		if err != nil {
			return nil, err
		}
		points[i] = point
	}

	p := aggregatePointG1(points)

	return p.Marshal(), nil
}

// AggregateBN256PublicKeys aggregate multiple BLS public keys into one.
//
// The order of the keys in the slice does not matter since the aggregation
// is commutative. The slice should not be empty.
// No check is performed on the input public keys. The input keys are guaranteed by
// the package constructors to be on the G2 subgroup.
// Input or output keys can be equal to the identity key. Note that any
// signature verified against the identity key is invalid (to avoid equivocation issues).
//
// The function returns:
//   - (nil, notBLSKeyError) if at least one key is not of type BLS BLS12-381
//   - (nil, blsAggregateEmptyListError) no keys are provided (input slice is empty)
//   - (aggregated_key, nil) otherwise
func AggregateBN256PublicKeys(keys []PublicKey) (PublicKey, error) {
	if len(keys) == 0 {
		return nil, bn256AggregateEmptyListError
	}
	points := make([]*bn256.G2, len(keys))
	for i := 0; i < len(keys); i++ {
		pk, ok := keys[i].(*pubKeyBLSBN256)
		if !ok {
			return nil, fmt.Errorf("wrong bn254 public key")
		}
		points[i] = pk.point
	}

	p := aggregatePointG2(points)

	return &pubKeyBLSBN256{point: p}, nil
}

// VerifyBN256SignatureOneMessage is a multi-signature verification that verifies a
// BLS signature of a single message against multiple BLS public keys.
//
// The input signature could be generated by aggregating multiple signatures of the
// message under multiple private keys. The public keys corresponding to the signing
// private keys are passed as input to this function.
// The caller must make sure the input public keys's proofs of possession have been
// verified prior to calling this function (or each input key is sum of public keys of
// which proofs of possession have been verified).
//
// The input hasher is the same hasher used to generate all initial signatures.
// The order of the public keys in the slice does not matter.
// Membership check is performed on the input signature but is not performed on the input
// public keys (membership is guaranteed by using the package functions).
// If the input public keys add up to the identity public key, the signature is invalid
// to avoid signature equivocation issues.
//
// This is a special case function of VerifyBLSSignatureManyMessages, using a single
// message and hasher.
//
// The function returns:
//   - (false, nilHasherError) if hasher is nil
//   - (false, invalidHasherSizeError) if hasher's output size is not 128 bytes
//   - (false, notBLSKeyError) if at least one key is not of type pubKeyBLSBLS12381
//   - (nil, blsAggregateEmptyListError) if input key slice is empty
//   - (false, error) if an unexpected error occurs
//   - (validity, nil) otherwise
func VerifyBN256SignatureOneMessage(
	pks []PublicKey, s Signature, message []byte, hasher hash.Hasher,
) (bool, error) {
	if len(pks) == 0 {
		return false, bn256AggregateEmptyListError
	}
	// public key list must be non empty, this is checked internally by AggregateBLSPublicKeys
	aggPk, err := AggregateBN256PublicKeys(pks)
	if err != nil {
		return false, fmt.Errorf("verify signature one message failed: %w", err)
	}

	return aggPk.Verify(s, message, hasher)
}

// BN256VerifyPOP verifies a proof of possession (PoP) for the receiver public key.
//
// The function internally uses the same KMAC hasher used to generate the PoP.
// The hasher is guaranteed to be orthogonal to any hasher used to generate signature
// or SPoCK proofs on this package.
// Note that verifying a PoP against an idenity public key fails.
//
// The function returns:
//   - (false, notBLSKeyError) if the input key is not of type BLS BLS12-381
//   - (validity, nil) otherwise
func BN256VerifyPOP(pk PublicKey, s Signature) (bool, error) {
	_, ok := pk.(*pubKeyBLSBN256)
	if !ok {
		return false, fmt.Errorf("wrong bn254 public key")
	}

	k256 := hash.NewKeccak_256()

	// verify the signature against the public key
	return pk.Verify(s, pk.Encode(), k256)
}

func aggregatePointG1(points []*bn256.G1) *bn256.G1 {
	if len(points) < 1 {
		return nil
	}
	g := new(bn256.G1).Set(points[0])
	// todo: use go routine
	for i := 1; i < len(points); i++ {
		g = g.Add(g, points[i])
	}
	return g
}

// aggregate points on the curve G2
func aggregatePointG2(points []*bn256.G2) *bn256.G2 {
	if len(points) < 1 {
		return nil
	}
	g := new(bn256.G2).Set(points[0])
	// todo: use go routine
	for i := 1; i < len(points); i++ {
		g = g.Add(g, points[i])
	}
	return g
}
