package loadtest

import "github.com/tendermint/tendermint/libs/common"

// KVStoreClientFactory creates load testing clients to interact with the
// built-in Tendermint kvstore ABCI application.
type KVStoreClientFactory struct{}

// KVStoreClient generates arbitrary transactions (random key=value pairs) to
// be sent to the kvstore ABCI application.
type KVStoreClient struct{}

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

func (f *KVStoreClientFactory) NewClient() (Client, error) {
	return &KVStoreClient{}, nil
}

func (c *KVStoreClient) GenerateTx() ([]byte, error) {
	k := []byte(common.RandStr(8))
	v := []byte(common.RandStr(8))
	return append(k, append([]byte("="), v...)...), nil
}
