package loadtest_test

import (
	"strings"
	"testing"
	"text/template"
	"time"

	"github.com/interchainio/tm-load-test/pkg/loadtest"
	"github.com/sirupsen/logrus"
	"github.com/tendermint/tendermint/abci/example/kvstore"
	rpctest "github.com/tendermint/tendermint/rpc/test"
)

const kvstoreHTTPConfig = `[master]
bind = "{{.MasterAddr}}"
expect_slaves = 2
expect_slaves_within = "1m"

[slave]
bind = "127.0.0.1:"
master = "http://{{.MasterAddr}}"
expect_master_within = "1m"
expect_start_within = "1m"

[test_network]
    [test_network.autodetect]
    enabled = false

    [[test_network.targets]]
    id = "host1"
    url = "{{.RPCAddr}}"

[clients]
type = "kvstore-http"
additional_params = ""
spawn = 10
spawn_rate = 10.0
max_interactions = 1
interaction_timeout = "11s"
max_test_time = "10m"
request_wait_min = "50ms"
request_wait_max = "100ms"
request_timeout = "5s"
`

const maxTestTime = 10 * time.Second

type runResult struct {
	summary *loadtest.Summary
	err     error
}

type testConfig struct {
	MasterAddr string
	RPCAddr    string
}

func generateConfig(tpl string) (string, error) {
	masterAddr, err := loadtest.ResolveBindAddr("127.0.0.1:")
	if err != nil {
		return "", err
	}

	// get the Tendermint RPC address
	testCfg := testConfig{
		MasterAddr: masterAddr,
		RPCAddr:    rpctest.GetConfig().RPC.ListenAddress,
	}

	var b strings.Builder
	t, err := template.New("loadtest-config").Parse(tpl)
	if err != nil {
		return "", err
	}

	if err := t.Execute(&b, &testCfg); err != nil {
		return "", err
	}

	return b.String(), nil
}

func init() {
	logrus.SetLevel(logrus.DebugLevel)
}

func TestKVStoreHTTPIntegrationWithTendermintNode(t *testing.T) {
	node := rpctest.StartTendermint(kvstore.NewKVStoreApplication())
	defer rpctest.StopTendermint(node)

	testCfg, err := generateConfig(kvstoreHTTPConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Log(testCfg)

	cfg, err := loadtest.ParseConfig(testCfg)
	if err != nil {
		t.Fatal("Failed to parse integration test config.", err)
	}

	masterChan := spawnRunner(cfg, loadtest.RunMasterWithConfig)
	slave1Chan := spawnRunner(cfg, loadtest.RunSlaveWithConfig)
	slave2Chan := spawnRunner(cfg, loadtest.RunSlaveWithConfig)

	// wait for load testing to successfully complete
	for finished := 0; finished < 3; {
		select {
		case r := <-masterChan:
			finished++
			if r.err != nil {
				t.Error(r.err)
			}
			if r.summary.Interactions != 20 {
				t.Errorf("expected 20 interactions from master, but got %d", r.summary.Interactions)
			}

		case r := <-slave1Chan:
			finished++
			if r.err != nil {
				t.Error(r.err)
			}
			if r.summary.Interactions != 10 {
				t.Errorf("expected 10 interactions from slave 1, but got %d", r.summary.Interactions)
			}

		case r := <-slave2Chan:
			finished++
			if r.err != nil {
				t.Error(r.err)
			}
			if r.summary.Interactions != 10 {
				t.Errorf("expected 10 interactions from slave 2, but got %d", r.summary.Interactions)
			}

		case <-time.After(maxTestTime):
			t.Fatal("Maximum test time expired")
		}
	}
}

func spawnRunner(cfg *loadtest.Config, runner func(*loadtest.Config) (*loadtest.Summary, error)) chan runResult {
	ch := make(chan runResult, 1)
	go func() {
		summary, err := runner(cfg)
		ch <- runResult{summary: summary, err: err}
	}()
	return ch
}
