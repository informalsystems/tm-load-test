package loadtest_test

import (
	"fmt"
	"net"
	"net/url"
	"testing"

	"github.com/interchainio/tm-load-test/pkg/loadtest"
	"github.com/tendermint/tendermint/abci/example/kvstore"
	rpctest "github.com/tendermint/tendermint/rpc/test"
)

func TestMasterSlaveHappyPath(t *testing.T) {
	app := kvstore.NewKVStoreApplication()
	node := rpctest.StartTendermint(app, rpctest.SuppressStdout, rpctest.RecreateConfig)
	defer rpctest.StopTendermint(node)

	freePort, err := getFreePort()
	if err != nil {
		t.Fatal(err)
	}

	cfg := testConfig()
	masterCfg := loadtest.MasterConfig{
		BindAddr:            fmt.Sprintf("localhost:%d", freePort),
		ExpectSlaves:        2,
		SlaveConnectTimeout: 10,
	}
	master := loadtest.NewMaster(&cfg, &masterCfg)
	masterErr := make(chan error, 1)
	go func() {
		masterErr <- master.Run()
	}()

	slaveCfg := loadtest.SlaveConfig{
		MasterAddr:           fmt.Sprintf("ws://localhost:%d", freePort),
		MasterConnectTimeout: 10,
	}
	slave1 := loadtest.NewSlave(&slaveCfg)
	slave1Err := make(chan error, 1)
	go func() {
		slave1Err <- slave1.Run()
	}()

	slave2 := loadtest.NewSlave(&slaveCfg)
	slave2Err := make(chan error, 1)
	go func() {
		slave2Err <- slave2.Run()
	}()

	for i := 0; i < 3; i++ {
		select {
		case err := <-masterErr:
			if err != nil {
				t.Fatal(err)
			}

		case err := <-slave1Err:
			if err != nil {
				t.Fatal(err)
			}

		case err := <-slave2Err:
			if err != nil {
				t.Fatal(err)
			}
		}
	}
}

func getRPCAddress() string {
	listenURL, err := url.Parse(rpctest.GetConfig().RPC.ListenAddress)
	if err != nil {
		panic(err)
	}
	return fmt.Sprintf("ws://localhost:%s/websocket", listenURL.Port())
}

func testConfig() loadtest.Config {
	return loadtest.Config{
		ClientFactory:     "kvstore",
		Connections:       1,
		Time:              5,
		SendPeriod:        1,
		Rate:              100,
		Size:              100,
		Count:             -1,
		BroadcastTxMethod: "async",
		Endpoints:         []string{getRPCAddress()},
	}
}

func getFreePort() (int, error) {
	addr, err := net.ResolveTCPAddr("tcp", "localhost:0")
	if err != nil {
		return 0, err
	}

	l, err := net.ListenTCP("tcp", addr)
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}
