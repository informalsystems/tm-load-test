package loadtest_test

import (
	"strings"
	"testing"
	"text/template"
	"time"

	"github.com/interchainio/tm-load-test/pkg/loadtest"
	"github.com/sirupsen/logrus"
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
request_wait_min = "5ms"
request_wait_max = "10ms"
request_timeout = "5s"
`

const kvstoreWebSocketsConfig = `[master]
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
    url = "{{.RPCAddr}}/websocket"

[clients]
type = "kvstore-websockets"
additional_params = ""
spawn = 10
spawn_rate = 10.0
max_interactions = {{.MaxInteractions}}
interaction_timeout = "11s"
max_test_time = "10m"
request_wait_min = "0ms"
request_wait_max = "0ms"
request_timeout = "5s"
`

const maxTestTime = 10 * time.Second

type runResult struct {
	summary *loadtest.Summary
	err     error
}

type testConfig struct {
	MasterAddr      string
	RPCAddr         string
	MaxInteractions int
}

type testCase struct {
	rawConfig       string
	maxInteractions int
	maxTestTime     time.Duration

	startTime  time.Time
	masterChan chan runResult
	slaveChans []chan runResult

	expectedMasterInteractions int64
	expectedSlaveInteractions  []int64
	expectedMinTestTime        time.Duration
}

func generateConfig(tpl string, maxInteractions int) (string, error) {
	masterAddr, err := loadtest.ResolveBindAddr("127.0.0.1:")
	if err != nil {
		return "", err
	}

	// get the Tendermint RPC address
	testCfg := testConfig{
		MasterAddr:      masterAddr,
		RPCAddr:         rpctest.GetConfig().RPC.ListenAddress,
		MaxInteractions: maxInteractions,
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
	runIntegrationTest(t, &testCase{
		rawConfig:                  kvstoreHTTPConfig,
		maxInteractions:            1,
		expectedMasterInteractions: 2 * 10 * 1, // no. of slaves * no. of clients * max interactions
		expectedSlaveInteractions:  []int64{10, 10},
	})
}

func TestKVStoreWebSocketsIntegrationWithTendermintNode(t *testing.T) {
	runIntegrationTest(t, &testCase{
		rawConfig:                  kvstoreWebSocketsConfig,
		maxInteractions:            1,
		expectedMasterInteractions: 2 * 10 * 1, // no. of slaves * no. of clients * max interactions
		expectedSlaveInteractions:  []int64{10, 10},
	})
}

func runIntegrationTest(t *testing.T, tc *testCase) {
	testCfg, err := generateConfig(tc.rawConfig, tc.maxInteractions)
	if err != nil {
		t.Fatal(err)
	}
	t.Log(testCfg)

	cfg, err := loadtest.ParseConfig(testCfg)
	if err != nil {
		t.Fatal("Failed to parse integration test config.", err)
	}

	tc.startTime = time.Now()
	tc.masterChan = spawnRunner(cfg, loadtest.RunMasterWithConfig)
	tc.slaveChans = make([]chan runResult, 2)
	tc.slaveChans[0] = spawnRunner(cfg, loadtest.RunSlaveWithConfig)
	tc.slaveChans[1] = spawnRunner(cfg, loadtest.RunSlaveWithConfig)
	waitAndAssertCorrect(t, tc)
}

func waitAndAssertCorrect(t *testing.T, tc *testCase) {
	// get the master's results
	select {
	case r := <-tc.masterChan:
		if r.err != nil {
			t.Error(r.err)
		}
		if tc.maxInteractions > -1 {
			if r.summary.Interactions != tc.expectedMasterInteractions {
				t.Errorf("expected %d interactions from master, but got %d", tc.expectedMasterInteractions, r.summary.Interactions)
			}
		}

	case <-time.After(maxTestTime):
		t.Error("maximum test time expired")
	}
	// get all slaves' results
	for i, slavec := range tc.slaveChans {
		select {
		case r := <-slavec:
			if r.err != nil {
				t.Error(r.err)
			}
			if tc.maxInteractions > -1 {
				if r.summary.Interactions != tc.expectedSlaveInteractions[i] {
					t.Errorf("expected %d interactions from slave %d, but got %d", tc.expectedSlaveInteractions[i], i, r.summary.Interactions)
				}
			}

		case <-time.After(maxTestTime):
			t.Error("maximum test time expired")
		}
	}
	if tc.maxInteractions == -1 {
		testTime := time.Since(tc.startTime)
		if testTime < tc.maxTestTime {
			t.Errorf("expected test to take a minimum of %s, but took %s", tc.startTime.String(), testTime.String())
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
