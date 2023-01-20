package loadtest

import (
	"fmt"
	"net"
	"net/url"
	"time"

	"github.com/informalsystems/tm-load-test/internal/logging"
)

// peerInfo is returned when polling the Tendermint RPC endpoint.
type peerInfo struct {
	Addr                string      // The address of the peer itself.
	Client              *httpClient // The client to use to query this peer's Tendermint RPC endpoint.
	PeerAddrs           []string    // The peers of this peer.
	SuccessfullyQueried bool        // Has this peer been successfully queried?
}

// Waits for the given minimum number of peers to be present on the network
// with the given starting list of peer addresses (or until the timeout
// expires). On success, returns the number of peers connected (for reporting),
// and on failure returns the relevant error.
//
// NOTE: this only works if the peers' RPC endpoints are bound to port 26657.
//
// TODO: Add in a stabilization time parameter (i.e. a minimum number of peers
// must be present when polled repeatedly for a period of time).
func waitForNetworkPeers(
	startingPeerAddrs []string,
	selectionMethod string,
	minDiscoveredPeers int,
	minPeerConnectivity int,
	maxReturnedPeers int,
	timeout time.Duration,
	logger logging.Logger,
) ([]string, error) {
	logger.Info(
		"Waiting for peers to connect",
		"minDiscoveredPeers", minDiscoveredPeers,
		"maxReturnedPeers", maxReturnedPeers,
		"timeout", fmt.Sprintf("%.2f seconds", timeout.Seconds()),
		"selectionMethod", selectionMethod,
	)

	cancelc := make(chan struct{}, 1)
	cancelTrap := trapInterrupts(func() { close(cancelc) }, logger)
	defer close(cancelTrap)
	startTime := time.Now()
	suppliedPeers := make(map[string]*peerInfo)
	for _, peerURL := range startingPeerAddrs {
		u, err := url.Parse(peerURL)
		if err != nil {
			return nil, fmt.Errorf("failed to parse peer URL %s: %s", peerURL, err)
		}

		// find the first IPv4 address for our supplied peer URL (this helps
		// with deduplication of peer addresses, since peer address books
		// usually just contain the IP addresses of other peers)
		peerIP, err := lookupFirstIPv4Addr(u.Hostname())
		if err != nil {
			return nil, fmt.Errorf("failed to resolve IP address for endpoint %s: %s", peerURL, err)
		}

		peerAddr := fmt.Sprintf("http://%s:26657", peerIP)
		client := newHttpRpcClient(peerAddr)
		suppliedPeers[peerAddr] = &peerInfo{
			Addr:      peerAddr,
			Client:    client,
			PeerAddrs: make([]string, 0),
		}
	}

	peers := make(map[string]*peerInfo)
	for a, p := range suppliedPeers {
		pc := *p
		peers[a] = &pc
	}

	for {
		remainingTimeout := timeout - time.Since(startTime)
		if remainingTimeout < 0 {
			return nil, fmt.Errorf("timed out waiting for Tendermint peer crawl to complete")
		}
		newPeers, err := getNetworkPeers(peers, remainingTimeout, cancelc, logger)
		if err != nil {
			return nil, err
		}
		// we only care if we've discovered more peers than in the previous attempt
		if len(newPeers) > len(peers) {
			peers = newPeers
		}
		peerCount := len(peers)
		peerConnectivity := getMinPeerConnectivity(peers)
		if peerCount >= minDiscoveredPeers && peerConnectivity >= minPeerConnectivity {
			logger.Info("All required peers connected", "count", peerCount, "minConnectivity", minPeerConnectivity)
			// we're done here
			return filterPeerMap(suppliedPeers, peers, selectionMethod, maxReturnedPeers, logger)
		} else {
			logger.Debug(
				"Peers discovered so far",
				"count", peerCount,
				"minConnectivity", peerConnectivity,
				"remainingTimeout", timeout-time.Since(startTime),
			)
			time.Sleep(1 * time.Second)
		}
	}
}

// Queries the given peers (in parallel) to construct a unique set of known
// peers across the entire network.
func getNetworkPeers(
	peers map[string]*peerInfo, // Any existing peers we know about already
	timeout time.Duration, // Maximum timeout for the entire operation
	cancelc chan struct{}, // Allows us to cancel the polling operations
	logger logging.Logger,
) (map[string]*peerInfo, error) {
	startTime := time.Now()
	peerInfoc := make(chan *peerInfo, len(peers))
	errc := make(chan error, len(peers))
	logger.Debug("Querying peers for more peers", "count", len(peers), "peers", getPeerAddrs(peers))
	// parallelize querying all the Tendermint nodes' peers
	for _, peer := range peers {
		// reset this every time
		peer.SuccessfullyQueried = false

		go func(peer_ *peerInfo) {
			netInfo, err := peer_.Client.netInfo()
			if err != nil {
				logger.Debug("Failed to query peer - skipping", "addr", peer_.Addr, "err", err)
				errc <- err
				return
			}
			peerAddrs := make([]string, 0)
			for _, peerInfo := range netInfo.Peers {
				peerAddrs = append(peerAddrs, fmt.Sprintf("http://%s:26657", peerInfo.RemoteIP))
			}
			peerInfoc <- &peerInfo{
				Addr:                peer_.Addr,
				Client:              peer_.Client,
				PeerAddrs:           peerAddrs,
				SuccessfullyQueried: true,
			}
		}(peer)
	}
	result := make(map[string]*peerInfo)
	expectedNetInfoResults := len(peers)
	receivedNetInfoResults := 0
	for {
		remainingTimeout := timeout - time.Since(startTime)
		if remainingTimeout < 0 {
			return nil, fmt.Errorf("timed out waiting for all peer network info to be returned")
		}
		select {
		case <-cancelc:
			return nil, fmt.Errorf("cancel signal received")
		case peerInfo := <-peerInfoc:
			result[peerInfo.Addr] = peerInfo
			receivedNetInfoResults++
		case <-errc:
			receivedNetInfoResults++
		case <-time.After(remainingTimeout):
			return nil, fmt.Errorf("timed out while waiting for all peer network info to be returned")
		}
		if receivedNetInfoResults >= expectedNetInfoResults {
			return resolvePeerMap(result), nil
		} else {
			// wait a little before polling  again
			time.Sleep(1 * time.Second)
		}
	}
}

func resolvePeerMap(peers map[string]*peerInfo) map[string]*peerInfo {
	result := make(map[string]*peerInfo)
	for addr, peer := range peers {
		result[addr] = peer

		for _, peerAddr := range peer.PeerAddrs {
			client := newHttpRpcClient(peerAddr)
			if _, exists := result[peerAddr]; !exists {
				result[peerAddr] = &peerInfo{
					Addr:      peerAddr,
					Client:    client,
					PeerAddrs: make([]string, 0),
				}
			}
		}
	}
	return result
}

func filterPeerMap(suppliedPeers, newPeers map[string]*peerInfo, selectionMethod string, maxCount int, logger logging.Logger) ([]string, error) {
	logger.Debug(
		"Filtering peer map",
		"suppliedPeers", suppliedPeers,
		"newPeers", newPeers,
		"selectionMethod", selectionMethod,
		"maxCount", maxCount,
	)
	result := make([]string, 0)
	for peerAddr := range newPeers {
		u, err := url.Parse(peerAddr)
		if err != nil {
			return nil, err
		}
		addr := fmt.Sprintf("ws://%s:26657/websocket", u.Hostname())
		switch selectionMethod {
		case SelectSuppliedEndpoints:
			// only add it to the result if it was in the original list
			if _, ok := suppliedPeers[peerAddr]; ok {
				result = append(result, addr)
			}
		case SelectDiscoveredEndpoints:
			// only add it to the result if it wasn't in the original list
			if _, ok := suppliedPeers[peerAddr]; !ok {
				result = append(result, addr)
			}
		default:
			// otherwise, always add it
			result = append(result, addr)
		}
		if maxCount > 0 && len(result) >= maxCount {
			break
		}
	}
	return result, nil
}

func getMinPeerConnectivity(peers map[string]*peerInfo) int {
	minPeers := len(peers)
	for _, peer := range peers {
		// we only care about peers we've successfully queried so far
		if !peer.SuccessfullyQueried {
			continue
		}
		peerCount := len(peer.PeerAddrs)
		if peerCount > 0 && peerCount < minPeers {
			minPeers = peerCount
		}
	}
	return minPeers
}

func getPeerAddrs(peers map[string]*peerInfo) []string {
	results := make([]string, 0)
	for _, peer := range peers {
		results = append(results, peer.Addr)
	}
	return results
}

func lookupFirstIPv4Addr(hostname string) (string, error) {
	ipRecords, err := net.LookupIP(hostname)
	if err != nil {
		return "", err
	}
	for _, ipRecord := range ipRecords {
		ipv4 := ipRecord.To4()
		if ipv4 != nil {
			return ipv4.String(), nil
		}
	}
	return "", fmt.Errorf("no IPv4 records for hostname: %s", hostname)
}
