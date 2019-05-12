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
max_test_time = "{{.MaxTestTime}}"
request_wait_min = "0ms"
request_wait_max = "0ms"
request_timeout = "5s"
`

const kvstoreAuthConfig = `[master]
bind = "{{.MasterAddr}}"
expect_slaves = 2
expect_slaves_within = "1m"

	[master.auth]
	enabled = true
	username = "testuser"
	password_hash = "$2a$08$rd8XKT/IO1a7MqcobrGX0epg05Z6/fEPopygo9G6yWuq4xLz8uhxq"

[slave]
bind = "127.0.0.1:"
master = "http://testuser:testpassword@{{.MasterAddr}}"
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
max_test_time = "{{.MaxTestTime}}"
request_wait_min = "0ms"
request_wait_max = "0ms"
request_timeout = "5s"
`

type runResult struct {
	summary *loadtest.Summary
	err     error
}

type testConfig struct {
	MasterAddr      string
	RPCAddr         string
	MaxInteractions int
	MaxTestTime     string
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
}

func generateConfig(tpl string, maxInteractions int, maxTestTime string) (string, error) {
	masterAddr, err := loadtest.ResolveBindAddr("127.0.0.1:")
	if err != nil {
		return "", err
	}

	// get the Tendermint RPC address
	testCfg := testConfig{
		MasterAddr:      masterAddr,
		RPCAddr:         rpctest.GetConfig().RPC.ListenAddress,
		MaxInteractions: maxInteractions,
		MaxTestTime:     maxTestTime,
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

func TestKVStoreHTTPIntegration(t *testing.T) {
	runIntegrationTest(t, &testCase{
		rawConfig:                  kvstoreHTTPConfig,
		maxInteractions:            1,
		maxTestTime:                time.Duration(time.Minute),
		expectedMasterInteractions: 2 * 10 * 1, // no. of slaves * no. of clients * max interactions
		expectedSlaveInteractions:  []int64{10, 10},
	})
}

func TestKVStoreWebSocketsIntegration(t *testing.T) {
	runIntegrationTest(t, &testCase{
		rawConfig:                  kvstoreWebSocketsConfig,
		maxInteractions:            1,
		maxTestTime:                time.Duration(time.Minute),
		expectedMasterInteractions: 2 * 10 * 1, // no. of slaves * no. of clients * max interactions
		expectedSlaveInteractions:  []int64{10, 10},
	})
}

func TestIntegrationWithAuth(t *testing.T) {
	runIntegrationTest(t, &testCase{
		rawConfig:                  kvstoreAuthConfig,
		maxInteractions:            1,
		maxTestTime:                time.Duration(60 * time.Second),
		expectedMasterInteractions: 2 * 10 * 1, // no. of slaves * no. of clients * max interactions
		expectedSlaveInteractions:  []int64{10, 10},
	})
}

func runIntegrationTest(t *testing.T, tc *testCase) {
	testCfg, err := generateConfig(tc.rawConfig, tc.maxInteractions, tc.maxTestTime.String())
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
		} else {
			if r.summary.Interactions == 0 {
				t.Errorf("expected some interactions master, but got none")
			}
		}

	// We give the master an extra few seconds' leeway here
	case <-time.After(tc.maxTestTime + (3 * time.Second)):
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
			} else {
				if r.summary.Interactions == 0 {
					t.Errorf("expected some interactions from slave %d, but got none", i)
				}
			}

		case <-time.After(tc.maxTestTime):
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
