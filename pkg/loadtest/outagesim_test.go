package loadtest_test

import (
	"fmt"
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

type outageSimTestCase struct {
	plan                  string
	user                  string
	password              string
	expectSyntaxError     bool
	expectRequestFailures bool
	expectedNode1Results  []outageSimTestResult
	expectedNode2Results  []outageSimTestResult
}

type outageSimTestResult struct {
	code       int
	nodeStatus loadtest.NodeStatus
}

func makeOutageSimHandler(resultc chan outageSimTestResult) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		u, p, _ := r.BasicAuth()
		if u != testOutageSimUser || p != testOutageSimPassword {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			resultc <- outageSimTestResult{code: http.StatusUnauthorized}
			return
		}
		if r.Method != "POST" {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			resultc <- outageSimTestResult{code: http.StatusMethodNotAllowed}
			return
		}
		body, err := ioutil.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			resultc <- outageSimTestResult{code: http.StatusInternalServerError}
			return
		}
		switch string(body) {
		case string(loadtest.NodeUp), string(loadtest.NodeDown):
			w.WriteHeader(http.StatusOK)
			resultc <- outageSimTestResult{code: http.StatusOK, nodeStatus: loadtest.NodeStatus(string(body))}

		default:
			http.Error(w, "Invalid request", http.StatusBadRequest)
			resultc <- outageSimTestResult{code: http.StatusBadRequest}
		}
	}
}

func TestOutageSimulator(t *testing.T) {
	ts1resultc := make(chan outageSimTestResult, 1)
	mux1 := http.NewServeMux()
	mux1.HandleFunc("/", makeOutageSimHandler(ts1resultc))
	ts1 := httptest.NewServer(mux1)
	defer ts1.Close()

	ts2resultc := make(chan outageSimTestResult, 1)
	mux2 := http.NewServeMux()
	mux2.HandleFunc("/", makeOutageSimHandler(ts2resultc))
	ts2 := httptest.NewServer(mux2)
	defer ts2.Close()

	nodeURLs := map[string]string{
		"node1": ts1.URL,
		"node2": ts2.URL,
	}

	testCases := []outageSimTestCase{
		// 0
		outageSimTestCase{
			plan: `
				node1=10ms:down,5ms:up,25ms:down
				node2=5ms:down,30ms:up
			`,
			user:     testOutageSimUser,
			password: testOutageSimPassword,
			expectedNode1Results: []outageSimTestResult{
				outageSimTestResult{
					code:       http.StatusOK,
					nodeStatus: loadtest.NodeDown,
				},
				outageSimTestResult{
					code:       http.StatusOK,
					nodeStatus: loadtest.NodeUp,
				},
				outageSimTestResult{
					code:       http.StatusOK,
					nodeStatus: loadtest.NodeDown,
				},
			},
			expectedNode2Results: []outageSimTestResult{
				outageSimTestResult{
					code:       http.StatusOK,
					nodeStatus: loadtest.NodeDown,
				},
				outageSimTestResult{
					code:       http.StatusOK,
					nodeStatus: loadtest.NodeUp,
				},
			},
		},
		// 1
		outageSimTestCase{
			plan: `
				node1=10ms:down,5ms:up
			`,
			user:     testOutageSimUser,
			password: testOutageSimPassword,
			expectedNode1Results: []outageSimTestResult{
				outageSimTestResult{
					code:       http.StatusOK,
					nodeStatus: loadtest.NodeDown,
				},
				outageSimTestResult{
					code:       http.StatusOK,
					nodeStatus: loadtest.NodeUp,
				},
			},
			expectedNode2Results: []outageSimTestResult{},
		},
		// 2
		outageSimTestCase{
			plan: `
				node1=10ms:down,5ms:up node2=10ms:down,5ms:up
			`,
			expectSyntaxError: true,
		},
		// 3
		outageSimTestCase{
			plan: `
				node1=node2=10ms:down,5ms:up
			`,
			expectSyntaxError: true,
		},
		// 4
		outageSimTestCase{
			plan: `
				node1=10ms:down,5ms:up,25ms:down
				node2=5ms:down,30ms:up
			`,
			user:                  testOutageSimUser,
			password:              "wrongpassword",
			expectRequestFailures: true,
			expectedNode1Results: []outageSimTestResult{
				outageSimTestResult{
					code: http.StatusUnauthorized,
				},
			},
			expectedNode2Results: []outageSimTestResult{
				outageSimTestResult{
					code: http.StatusUnauthorized,
				},
			},
		},
	}

testCaseLoop:
	for i, tc := range testCases {
		sim, err := loadtest.NewOutageSimulator(
			tc.plan,
			tc.user,
			tc.password,
			nodeURLs,
		)
		if err != nil {
			if tc.expectSyntaxError {
				continue testCaseLoop
			}
			t.Fatalf("test case %d: %v", i, err)
		}
		errc := make(chan error, 1)
		sim.Start(errc)

		node1Results := make([]outageSimTestResult, 0)
		node2Results := make([]outageSimTestResult, 0)
	waitLoop:
		for {
			select {
			case err := <-errc:
				if !tc.expectRequestFailures {
					t.Fatalf("test case %d: %v", i, err)
				}

			case r := <-ts1resultc:
				node1Results = append(node1Results, r)
				if len(node1Results) == len(tc.expectedNode1Results) && len(node2Results) == len(tc.expectedNode2Results) {
					break waitLoop
				}

			case r := <-ts2resultc:
				node2Results = append(node2Results, r)
				if len(node1Results) == len(tc.expectedNode1Results) && len(node2Results) == len(tc.expectedNode2Results) {
					break waitLoop
				}

			case <-time.After(1 * time.Second):
				t.Fatalf("test case %d: test timed out", i)
			}
		}
		sim.Wait()

		if !testOutageResultsEqual(tc.expectedNode1Results, node1Results) {
			t.Error(fmt.Sprintf("test case %d:", i), "got node 1 results:", node1Results, "but expected:", tc.expectedNode1Results)
		}
		if !testOutageResultsEqual(tc.expectedNode2Results, node2Results) {
			t.Error(fmt.Sprintf("test case %d:", i), "got node 2 results:", node2Results, "but expected:", tc.expectedNode2Results)
		}
	}
}

func testOutageResultsEqual(expected, actual []outageSimTestResult) bool {
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
