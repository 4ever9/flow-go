package badger_test

import (
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"testing"

	"github.com/dgraph-io/badger/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dapperlabs/flow-go/storage"
	badgerstorage "github.com/dapperlabs/flow-go/storage/badger"
	"github.com/dapperlabs/flow-go/utils/unittest"
)

func TestTransactions(t *testing.T) {
	dir := filepath.Join(os.TempDir(), fmt.Sprintf("flow-test-db-%d", rand.Uint64()))
	defer os.RemoveAll(dir)
	db, err := badger.Open(badger.DefaultOptions(dir).WithLogger(nil))
	require.Nil(t, err)

	store := badgerstorage.NewTransactions(db)

	expected := unittest.TransactionFixture()
	err = store.Insert(&expected)
	require.Nil(t, err)

	actual, err := store.ByFingerprint(expected.Fingerprint())
	require.Nil(t, err)

	assert.Equal(t, &expected, actual)

	err = store.Remove(expected.Hash())
	require.NoError(t, err)

	// should fail since this was just deleted
	_, err = store.ByFingerprint(expected.Fingerprint())
	if assert.Error(t, err) {
		assert.True(t, errors.Is(err, storage.NotFoundErr))
	}
}
