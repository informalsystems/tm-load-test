package loadtest

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/interchainio/tm-load-test/internal/logging"
)

// DefaultOutageSimTimeout is the default timeout before considering a request
// to the outage simulator on a particular node to have failed.
const DefaultOutageSimTimeout = time.Duration(3 * time.Second)

// NodeStatus allows us to model whether a node is up or down.
type NodeStatus string

// The various states in which a node can be, in relation to the outage
// simulator.
const (
	NodeUp   NodeStatus = "up"
	NodeDown NodeStatus = "down"
)

// OutageSimulator is a client for interacting with tm-outage-sim-server on
// behalf of one or more target network nodes.
type OutageSimulator struct {
	nodes map[string]*NodeOutageSimulator
}

// NodeOutageSimulator is a client for interacting with tm-outage-sim-server on
// behalf of a single target network node.
type NodeOutageSimulator struct {
	logger        logging.Logger
	statusUpdates *NodeStatusUpdate // The first status update in the linked list.
	donec         chan struct{}     // Closed when this node's outage simulation is complete.
}

// NodeStatusUpdate is a linked list that keeps track of a wait time and the
// desired node status at the end of that wait time. It also keeps track of the
// next outage to be executed after this one.
type NodeStatusUpdate struct {
	Wait   time.Duration
	Status NodeStatus
	Next   *NodeStatusUpdate // The status update to be executed after this one.

	logger       logging.Logger
	client       *http.Client // An HTTP client with which to make the requests.
	outageSimURL *url.URL     // The URL to which to make this status update.
	username     string
	password     string
	cancelc      chan struct{} // Closed when this update needs to be cancelled.
	donec        chan struct{} // Closed if this is the final update in a chain and it terminates.
}

//
// OutageSimulator
//

// NewOutageSimulator constructs an outage simulator for multiple test targets.
func NewOutageSimulator(plan, username, password string, nodeURLs map[string]string) (*OutageSimulator, error) {
	nodePlans, err := parseOutagePlan(plan)
	if err != nil {
		return nil, err
	}
	nodes := make(map[string]*NodeOutageSimulator)
	for moniker, nodePlan := range nodePlans {
		nodeURL, exists := nodeURLs[moniker]
		if !exists {
			return nil, fmt.Errorf("unrecognized node moniker in outage simulation plan: %s", moniker)
		}
		outageSimURL, err := url.Parse(nodeURL)
		if err != nil {
			return nil, fmt.Errorf("failed to parse outage simulator URL for target %s: %v", moniker, err)
		}
		nodes[moniker], err = NewNodeOutageSimulator(moniker, outageSimURL, username, password, nodePlan)
		if err != nil {
			return nil, err
		}
	}
	return &OutageSimulator{
		nodes: nodes,
	}, nil
}

// Start kicks off the outage simulator, and provides feedback via the given
// error channel.
func (s *OutageSimulator) Start(errc chan error) {
	for _, node := range s.nodes {
		node.Start(errc)
	}
}

// Cancel will cancel all node outage simulators.
func (s *OutageSimulator) Cancel() {
	for _, node := range s.nodes {
		node.Cancel()
	}
}

// Wait blocks the current goroutine until all of the nodes' update chains have
// terminated.
func (s *OutageSimulator) Wait() {
	for _, node := range s.nodes {
		node.Wait()
	}
}

// Parses the given plan into a map of node IDs to node-specific outage plans.
// The plan format looks like the following:
//
//    <node_moniker>=<wait_duration>:<status>,<wait_duration>:<status>,...
//    node1=3m:down,8m:up,2m:down,30s:up
//    node2=1m:down,12m:up,3m:down,10s:up
//
func parseOutagePlan(plan string) (nodePlan map[string]string, err error) {
	nodePlan = make(map[string]string)
	lines := strings.Split(plan, "\n")
	for lineNo, line := range lines {
		tline := strings.Trim(line, " \r\n\t")
		if len(tline) > 0 {
			parts := strings.Split(tline, "=")
			if len(parts) != 2 {
				err = fmt.Errorf("syntax error on line %d of outage plan configuration", lineNo)
				return
			}
			tmoniker, tnodePlan := strings.Trim(parts[0], " \t"), strings.Trim(parts[1], " \t")
			if len(tmoniker) == 0 {
				err = fmt.Errorf("syntax error on line %d of outage plan configuration: missing moniker", lineNo)
				return
			}
			if len(tnodePlan) == 0 {
				err = fmt.Errorf("syntax error on line %d of outage plan configuration: missing node plan", lineNo)
				return
			}
			if _, exists := nodePlan[tmoniker]; exists {
				err = fmt.Errorf("syntax error on line %d of outage plan configuration: duplicate node plan for moniker \"%s\"", lineNo, tmoniker)
				return
			}
			nodePlan[tmoniker] = tnodePlan
		}
	}
	return
}

//
// NodeOutageSimulator
//

// NewNodeOutageSimulator parses the given plan to produce either a single-node
// outage simulator, or an error on failure.
func NewNodeOutageSimulator(moniker string, outageSimURL *url.URL, username, password, plan string) (*NodeOutageSimulator, error) {
	var curUpdate *NodeStatusUpdate
	lastStatus := NodeUp
	sim := &NodeOutageSimulator{
		logger: logging.NewLogrusLogger(fmt.Sprintf("outagesim-%s", moniker)),
		donec:  make(chan struct{}),
	}
	client := &http.Client{Timeout: DefaultOutageSimTimeout}

	statusUpdates := strings.Split(plan, ",")
	for i, statusUpdate := range statusUpdates {
		wait, newStatus, err := parseNodeOutagePlan(statusUpdate)
		if err != nil {
			return nil, fmt.Errorf("error in outage %d: %v", i, err)
		}
		if i > 0 && newStatus == lastStatus {
			return nil, fmt.Errorf("error in outage %d: status is same as previous status", i)
		}
		lastStatus = newStatus

		newUpdate := NewNodeStatusUpdate(client, outageSimURL, username, password, wait, newStatus, sim.donec, sim.logger)
		if curUpdate == nil {
			curUpdate = newUpdate
			sim.statusUpdates = curUpdate
		} else {
			curUpdate.Next = newUpdate
			// advance our cursor in the linked list
			curUpdate = curUpdate.Next
		}
	}
	return sim, nil
}

func parseNodeOutagePlan(plan string) (wait time.Duration, newStatus NodeStatus, err error) {
	parts := strings.Split(strings.Trim(plan, " "), ":")
	if len(parts) != 2 {
		err = fmt.Errorf("expecting a time and a status separated by a single colon")
		return
	}
	wait, err = time.ParseDuration(parts[0])
	if err != nil {
		err = fmt.Errorf("%v", err)
		return
	}
	if parts[1] != string(NodeUp) && parts[1] != string(NodeDown) {
		err = fmt.Errorf("expecting either \"%s\" or \"%s\"", NodeUp, NodeDown)
		return
	}
	newStatus = NodeStatus(parts[1])
	return
}

// Start kicks off the chain of outage instructions. If an error occurs, the
// chain's execution will be terminated and the error will be returned through
// the given channel.
func (s *NodeOutageSimulator) Start(errc chan error) {
	if s.statusUpdates != nil {
		s.statusUpdates.Start(errc)
	}
}

// Cancel will cancel all of this node's status updates immediately, preventing
// any future ones from completing.
func (s *NodeOutageSimulator) Cancel() {
	if s.statusUpdates != nil {
		s.statusUpdates.Cancel()
	}
}

// Wait will wait until the node outage simulator's update chain completes.
func (s *NodeOutageSimulator) Wait() {
	if s.statusUpdates != nil {
		s.statusUpdates.Join()
	}
}

//
// NodeStatusUpdate
//

// NewNodeStatusUpdate constructs an outage from the given parameters.
func NewNodeStatusUpdate(
	client *http.Client,
	outageSimURL *url.URL,
	username, password string,
	wait time.Duration,
	status NodeStatus,
	donec chan struct{},
	logger logging.Logger,
) *NodeStatusUpdate {

	return &NodeStatusUpdate{
		Wait:         wait,
		Status:       status,
		logger:       logger,
		client:       client,
		outageSimURL: outageSimURL,
		username:     username,
		password:     password,
		cancelc:      make(chan struct{}),
		donec:        donec,
	}
}

// Start will kick off the chain of updates from this one onwards, sequentially.
func (u *NodeStatusUpdate) Start(errc chan error) {
	go func() {
		defer close(u.donec)
		update := u
		for update != nil {
			if err := update.waitAndExecute(); err != nil {
				errc <- err
				return
			}
			update = update.Next
		}
	}()
}

// Cancel will cancel this particular update, and will cascade the cancellations
// to all subsequent updates.
func (u *NodeStatusUpdate) Cancel() {
	close(u.cancelc)
	// cascade the cancellation
	if u.Next != nil {
		u.Next.Cancel()
	}
}

// Join will pause the current goroutine until either the status update chain
// completes.
func (u *NodeStatusUpdate) Join() {
	<-u.donec
}

func (u *NodeStatusUpdate) waitAndExecute() (err error) {
	u.logger.Debug("Waiting to execute outage", "wait", u.Wait.String())
	ticker := time.NewTicker(u.Wait)
	defer ticker.Stop()
	select {
	case <-ticker.C:
		err = u.execute()
		return

	case <-u.cancelc:
		return
	}
}

func (u *NodeStatusUpdate) execute() (err error) {
	u.logger.Info("Attempting to update node status", "status", u.Status)
	req, err := http.NewRequest("POST", u.outageSimURL.String(), strings.NewReader(string(u.Status)))
	if err != nil {
		u.logger.Error("Failed to construct request", "err", err)
		return
	}
	defer req.Body.Close()
	req.SetBasicAuth(u.username, u.password)
	res, err := u.client.Do(req)
	if err != nil {
		u.logger.Error("Failed to execute request", "err", err)
		return
	}
	defer res.Body.Close()
	if res.StatusCode >= 400 {
		body, _ := ioutil.ReadAll(res.Body)
		err = fmt.Errorf("request to outage simulator failed with status code %d: %s", res.StatusCode, string(body))
		u.logger.Error("Unacceptable response from outage simulator server", "err", err)
	}
	return
}
