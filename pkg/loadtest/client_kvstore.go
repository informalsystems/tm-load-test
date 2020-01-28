package loadtest

import (
	"encoding/binary"
	"fmt"
	"sync"

	"github.com/tendermint/tendermint/libs/common"
)

const minKVStoreTxSize int = 40

// KVStoreClientFactory creates load testing clients to interact with the
// built-in Tendermint kvstore ABCI application.
type KVStoreClientFactory struct {
	mtx         sync.Mutex
	clientCount uint32
}

// KVStoreClient generates arbitrary transactions (random key=value pairs) to
// be sent to the kvstore ABCI application.
type KVStoreClient struct {
	keyPrefix []byte
	keyLen    int
	valueLen  int
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
	// this will ensure that the key length is at least 19 bytes (4 bytes for
	// the client ID, and another 15 random bytes)
	if cfg.Size < minKVStoreTxSize {
		return fmt.Errorf("transaction size must be at least %d bytes", minKVStoreTxSize)
	}
	return nil
}

func (f *KVStoreClientFactory) NewClient(cfg Config) (Client, error) {
	// calculate how long each key/value pair must be to facilitate the
	// configured transaction size
	keyLen := (cfg.Size / 2) - 1
	// we need to cater for the "=" symbol in between (minimum 20 bytes)
	valueLen := cfg.Size - keyLen - 1
	// subtract 4 bytes to make space for the client ID prefix
	// TODO: Should we consider any alternative ways of constructing a key prefix?
	keyPrefix := make([]byte, 4)
	binary.LittleEndian.PutUint32(keyPrefix, f.nextClientID())
	keyLen -= 4
	return &KVStoreClient{
		keyPrefix: keyPrefix,
		keyLen:    keyLen,
		valueLen:  valueLen,
	}, nil
}

func (f *KVStoreClientFactory) nextClientID() uint32 {
	f.mtx.Lock()
	defer f.mtx.Unlock()
	f.clientCount += 1
	return f.clientCount - 1
}

func (c *KVStoreClient) GenerateTx() ([]byte, error) {
	k := append(c.keyPrefix, []byte(common.RandStr(c.keyLen))...)
	v := []byte(common.RandStr(c.valueLen))
	return append(k, append([]byte("="), v...)...), nil
}
