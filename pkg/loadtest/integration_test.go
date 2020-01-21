package loadtest_test

import (
	"encoding/csv"
	"fmt"
	"io/ioutil"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/interchainio/tm-load-test/pkg/loadtest"
	"github.com/tendermint/tendermint/abci/example/kvstore"
	rpctest "github.com/tendermint/tendermint/rpc/test"
)

const totalTxsPerSlave = 50

type aggregateStats struct {
	totalTime float64
	totalTxs  int
	avgTxRate float64
}

func (s *aggregateStats) String() string {
	return fmt.Sprintf(
		"aggregateStats{totalTime: %.3f, totalTxs: %d, avgTxRate: %.3f}",
		s.totalTime,
		s.totalTxs,
		s.avgTxRate,
	)
}

func TestMasterSlaveHappyPath(t *testing.T) {
	app := kvstore.NewKVStoreApplication()
	node := rpctest.StartTendermint(app, rpctest.SuppressStdout, rpctest.RecreateConfig)
	defer rpctest.StopTendermint(node)

	freePort, err := getFreePort()
	if err != nil {
		t.Fatal(err)
	}

	tempDir, err := ioutil.TempDir("", "tmloadtest-masterslavehappypath")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	expectedTotalTxs := totalTxsPerSlave * 2
	cfg := testConfig(tempDir)
	masterCfg := loadtest.MasterConfig{
		BindAddr:            fmt.Sprintf("localhost:%d", freePort),
		ExpectSlaves:        2,
		SlaveConnectTimeout: 10,
		ShutdownWait:        1,
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
	slave1, err := loadtest.NewSlave(&slaveCfg)
	if err != nil {
		t.Fatal(err)
	}
	slave1Err := make(chan error, 1)
	go func() {
		slave1Err <- slave1.Run()
	}()

	slave2, err := loadtest.NewSlave(&slaveCfg)
	if err != nil {
		t.Fatal(err)
	}
	slave2Err := make(chan error, 1)
	go func() {
		slave2Err <- slave2.Run()
	}()

	slave1Stopped := false
	slave2Stopped := false
	metricsTested := false

	for i := 0; i < 3; i++ {
		select {
		case err := <-masterErr:
			if err != nil {
				t.Fatal(err)
			}

		case err := <-slave1Err:
			slave1Stopped = true
			if err != nil {
				t.Fatal(err)
			}

		case err := <-slave2Err:
			slave2Stopped = true
			if err != nil {
				t.Fatal(err)
			}

		case <-time.After(time.Duration(cfg.Time*2) * time.Second):
			t.Fatal("Timed out waiting for test to complete")
		}

		// at this point the master should be waiting a little
		if slave1Stopped && slave2Stopped && !metricsTested {
			metricsTested = true
			// grab the prometheus metrics from the master
			resp, err := http.Get(fmt.Sprintf("http://localhost:%d/metrics", freePort))
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != 200 {
				t.Fatalf("Expected status code 200 from Prometheus endpoint, but got %d", resp.StatusCode)
			}
			body, err := ioutil.ReadAll(resp.Body)
			if err != nil {
				t.Fatal("Failed to read response body from Prometheus endpoint:", err)
			}
			for _, line := range strings.Split(string(body), "\n") {
				if strings.HasPrefix(line, "tmloadtest_master_total_txs") {
					parts := strings.Split(line, " ")
					if len(parts) < 2 {
						t.Fatal("Invalid Prometheus metrics format")
					}
					txCount, err := strconv.Atoi(parts[1])
					if err != nil {
						t.Fatal(err)
					}
					if txCount != expectedTotalTxs {
						t.Fatalf("Expected %d transactions to have been recorded by the master, but got %d", expectedTotalTxs, txCount)
					}
				}
			}
		}
	}

	if !metricsTested {
		t.Fatal("Expected to have tested Prometheus metrics, but did not")
	}

	// ensure the aggregate stats were generated and computed correctly
	stats, err := parseStats(cfg.StatsOutputFile)
	if err != nil {
		t.Fatal("Failed to parse output stats", err)
	}
	t.Logf("Got aggregate statistics from CSV: %v", stats)
	if stats.totalTxs != (totalTxsPerSlave * 2) {
		t.Fatalf("Expected %d transactions to have been recorded in aggregate stats, but got %d", expectedTotalTxs, stats.totalTxs)
	}
	if !floatsEqualWithTolerance(stats.avgTxRate, float64(stats.totalTxs)/stats.totalTime, 0.1) {
		t.Fatalf(
			"Average transaction rate (%.3f) does not compute from total time (%.3f) and total transactions (%d)",
			stats.avgTxRate,
			stats.totalTime,
			stats.totalTxs,
		)
	}
}

func getRPCAddress() string {
	listenURL, err := url.Parse(rpctest.GetConfig().RPC.ListenAddress)
	if err != nil {
		panic(err)
	}
	return fmt.Sprintf("ws://localhost:%s/websocket", listenURL.Port())
}

func testConfig(tempDir string) loadtest.Config {
	return loadtest.Config{
		ClientFactory:        "kvstore",
		Connections:          1,
		Time:                 5,
		SendPeriod:           1,
		Rate:                 100,
		Size:                 100,
		Count:                totalTxsPerSlave,
		BroadcastTxMethod:    "async",
		Endpoints:            []string{getRPCAddress()},
		EndpointSelectMethod: loadtest.SelectSuppliedEndpoints,
		StatsOutputFile:      path.Join(tempDir, "stats.csv"),
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

func parseStats(filename string) (*aggregateStats, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	reader := csv.NewReader(f)
	records, err := reader.ReadAll()
	if err != nil {
		return nil, err
	}

	if len(records) < 3 {
		return nil, fmt.Errorf("expected at least 3 records in aggregate stats CSV, but got %d", len(records))
	}
	stats := &aggregateStats{}
	for _, record := range records {
		if len(record) > 0 {
			if len(record) < 3 {
				return nil, fmt.Errorf("expected at least 3 columns for each non-empty row in aggregate stats CSV")
			}
			switch record[0] {
			case "total_txs":
				totalTxs, err := strconv.ParseInt(record[1], 10, 32)
				if err != nil {
					return nil, err
				}
				stats.totalTxs = int(totalTxs)

			case "total_time":
				stats.totalTime, err = strconv.ParseFloat(record[1], 64)
				if err != nil {
					return nil, err
				}

			case "avg_tx_rate":
				stats.avgTxRate, err = strconv.ParseFloat(record[1], 64)
				if err != nil {
					return nil, err
				}
			}
		}
	}

	return stats, nil
}

func floatsEqualWithTolerance(a, b, tolerance float64) bool {
	return math.Abs(a-b) < tolerance
}
