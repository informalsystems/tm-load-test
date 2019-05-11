package loadtest_test

import (
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/interchainio/tm-load-test/pkg/loadtest"
)

const (
	testOutageSimUser     = "testuser"
	testOutageSimPassword = "testpassword"
)

type testOutageResult struct {
	code       int
	nodeStatus loadtest.NodeStatus
}

func makeOutageSimHandler(resultc chan testOutageResult) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		u, p, _ := r.BasicAuth()
		if u != testOutageSimUser || p != testOutageSimPassword {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			resultc <- testOutageResult{code: http.StatusUnauthorized}
			return
		}
		if r.Method != "POST" {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			resultc <- testOutageResult{code: http.StatusMethodNotAllowed}
			return
		}
		body, err := ioutil.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			resultc <- testOutageResult{code: http.StatusInternalServerError}
			return
		}
		switch string(body) {
		case string(loadtest.NodeUp), string(loadtest.NodeDown):
			w.WriteHeader(http.StatusOK)
			resultc <- testOutageResult{code: http.StatusOK, nodeStatus: loadtest.NodeStatus(string(body))}

		default:
			http.Error(w, "Invalid request", http.StatusBadRequest)
			resultc <- testOutageResult{code: http.StatusBadRequest}
		}
	}
}

func TestOutageSimulatorHappyPath(t *testing.T) {
	ts1resultc := make(chan testOutageResult, 1)
	mux1 := http.NewServeMux()
	mux1.HandleFunc("/", makeOutageSimHandler(ts1resultc))
	ts1 := httptest.NewServer(mux1)
	defer ts1.Close()

	ts2resultc := make(chan testOutageResult, 1)
	mux2 := http.NewServeMux()
	mux2.HandleFunc("/", makeOutageSimHandler(ts2resultc))
	ts2 := httptest.NewServer(mux2)
	defer ts2.Close()

	nodeURLs := map[string]string{
		"node1": ts1.URL,
		"node2": ts2.URL,
	}
	sim, err := loadtest.NewOutageSimulator(
		`
			node1=10ms:down,5ms:up,25ms:down
			node2=5ms:down,30ms:up
		`,
		testOutageSimUser,
		testOutageSimPassword,
		nodeURLs,
	)
	if err != nil {
		t.Fatal(err)
	}
	errc := make(chan error, 1)
	sim.Start(errc)

	node1Results := make([]testOutageResult, 0)
	node2Results := make([]testOutageResult, 0)
waitLoop:
	for {
		select {
		case err := <-errc:
			t.Fatal(err)

		case r := <-ts1resultc:
			node1Results = append(node1Results, r)
			if len(node1Results) == 3 && len(node2Results) == 2 {
				break waitLoop
			}

		case r := <-ts2resultc:
			node2Results = append(node2Results, r)
			if len(node1Results) == 3 && len(node2Results) == 2 {
				break waitLoop
			}

		case <-time.After(1 * time.Second):
			t.Fatal("test timed out")
		}
	}
	sim.Wait()

	expectedNode1Results := []testOutageResult{
		testOutageResult{
			code:       http.StatusOK,
			nodeStatus: loadtest.NodeDown,
		},
		testOutageResult{
			code:       http.StatusOK,
			nodeStatus: loadtest.NodeUp,
		},
		testOutageResult{
			code:       http.StatusOK,
			nodeStatus: loadtest.NodeDown,
		},
	}
	expectedNode2Results := []testOutageResult{
		testOutageResult{
			code:       http.StatusOK,
			nodeStatus: loadtest.NodeDown,
		},
		testOutageResult{
			code:       http.StatusOK,
			nodeStatus: loadtest.NodeUp,
		},
	}

	if !testOutageResultsEqual(expectedNode1Results, node1Results) {
		t.Error("got node 1 results:", node1Results, "but expected:", expectedNode1Results)
	}
	if !testOutageResultsEqual(expectedNode2Results, node2Results) {
		t.Error("got node 2 results:", node2Results, "but expected:", expectedNode2Results)
	}
}

func testOutageResultsEqual(expected, actual []testOutageResult) bool {
	if len(expected) != len(actual) {
		return false
	}

	for i, e := range expected {
		a := actual[i]
		if e != a {
			return false
		}
	}

	return true
}
