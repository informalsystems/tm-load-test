package loadtest_test

import (
	"os"
	"testing"

	"github.com/tendermint/tendermint/abci/example/kvstore"
	rpctest "github.com/tendermint/tendermint/rpc/test"
)

func TestMain(m *testing.M) {
	// start a tendermint node (and kvstore) in the background to test against
	app := kvstore.NewKVStoreApplication()
	node := rpctest.StartTendermint(app)
	defer rpctest.StopTendermint(node)
	os.Exit(m.Run())
}
