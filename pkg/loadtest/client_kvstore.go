package loadtest

import (
	"fmt"

	"github.com/tendermint/iavl/common"
)

// The Tendermint common.RandStr method can effectively generate human-readable
// (alphanumeric) strings from a set of 62 characters. We aim here with the
// KVStore client to generate unique client IDs as well as totally unique keys
// for all transactions. Values are not so important.
const KVStoreClientIDLen int = 5 // Allows for 6,471,002 random client IDs (62C5)
const kvstoreMinValueLen int = 1 // We at least need 1 character in a key/value pair's value.

// This is a map of nCr where n=62 and r varies from 0 through 15. It gives the
// maximum number of unique transaction IDs that can be accommodated with a
// given key suffix length.
var kvstoreMaxTxsByKeySuffixLen = []uint64{
	0,              // 0
	62,             // 1
	1891,           // 2
	37820,          // 3
	557845,         // 4
	6471002,        // 5
	61474519,       // 6
	491796152,      // 7
	3381098545,     // 8
	20286591270,    // 9
	107518933731,   // 10
	508271323092,   // 11
	2160153123141,  // 12
	8308281242850,  // 13
	29078984349975, // 14
	93052749919920, // 15
}

// KVStoreClientFactory creates load testing clients to interact with the
// built-in Tendermint kvstore ABCI application.
type KVStoreClientFactory struct{}

// KVStoreClient generates arbitrary transactions (random key=value pairs) to
// be sent to the kvstore ABCI application. The keys are structured as follows:
//
// `[client_id][tx_id]=[tx_id]`
//
// where each value (`client_id` and `tx_id`) is padded with 0s to meet the
// transaction size requirement.
type KVStoreClient struct {
	keyPrefix    []byte // Contains the client ID
	keySuffixLen int
	valueLen     int
}

var _ ClientFactory = (*KVStoreClientFactory)(nil)
var _ Client = (*KVStoreClient)(nil)

func init() {
	if err := RegisterClientFactory("kvstore", NewKVStoreClientFactory()); err != nil {
		panic(err)
	}
}

func NewKVStoreClientFactory() *KVStoreClientFactory {
	return &KVStoreClientFactory{}
}

func (f *KVStoreClientFactory) ValidateConfig(cfg Config) error {
	maxTxsPerEndpoint := cfg.MaxTxsPerEndpoint()
	if maxTxsPerEndpoint < 1 {
		return fmt.Errorf("cannot calculate an appropriate maximum number of transactions per endpoint (got %d)", maxTxsPerEndpoint)
	}
	minKeySuffixLen, err := requiredKVStoreSuffixLen(maxTxsPerEndpoint)
	if err != nil {
		return err
	}
	// "[client_id][random_suffix]=[value]"
	minTxSize := KVStoreClientIDLen + minKeySuffixLen + 1 + kvstoreMinValueLen
	if cfg.Size < minTxSize {
		return fmt.Errorf("transaction size %d is too small for given parameters (should be at least %d bytes)", cfg.Size, minTxSize)
	}
	return nil
}

func (f *KVStoreClientFactory) NewClient(cfg Config) (Client, error) {
	keyPrefix := []byte(common.RandStr(KVStoreClientIDLen))
	keySuffixLen, err := requiredKVStoreSuffixLen(cfg.MaxTxsPerEndpoint())
	if err != nil {
		return nil, err
	}
	keyLen := len(keyPrefix) + keySuffixLen
	// value length = key length - 1 (to cater for "=" symbol)
	valueLen := cfg.Size - keyLen - 1
	return &KVStoreClient{
		keyPrefix:    keyPrefix,
		keySuffixLen: keySuffixLen,
		valueLen:     valueLen,
	}, nil
}

func requiredKVStoreSuffixLen(maxTxCount uint64) (int, error) {
	for l, maxTxs := range kvstoreMaxTxsByKeySuffixLen {
		if maxTxCount < maxTxs {
			if l+1 > len(kvstoreMaxTxsByKeySuffixLen) {
				return -1, fmt.Errorf("cannot cater for maximum tx count of %d (too many unique transactions, suffix length %d)", maxTxCount, l+1)
			}
			// we use l+1 to minimize collision probability
			return l + 1, nil
		}
	}
	return -1, fmt.Errorf("cannot cater for maximum tx count of %d (too many unique transactions)", maxTxCount)
}

func (c *KVStoreClient) GenerateTx() ([]byte, error) {
	k := append(c.keyPrefix, []byte(common.RandStr(c.keySuffixLen))...)
	v := []byte(common.RandStr(c.valueLen))
	return append(k, append([]byte("="), v...)...), nil
}
