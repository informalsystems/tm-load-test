package loadtest

import (
	"fmt"
	"net/url"
	"time"

	"github.com/interchainio/tm-load-test/internal/logging"
	"github.com/tendermint/tendermint/rpc/client"
)

// tendermintPeerInfo is returned when polling the Tendermint RPC endpoint.
type tendermintPeerInfo struct {
	Addr      string        // The address of the peer itself.
	Client    client.Client // The client to use to query this peer's Tendermint RPC endpoint.
	PeerAddrs []string      // The peers of this peer.
}

// Waits for the given minimum number of peers to be present on the Tendermint
// network with the given starting list of peer addresses (or until the timeout
// expires). On success, returns the number of peers connected (for reporting),
// and on failure returns the relevant error.
//
// NOTE: this only works if the peers' RPC endpoints are bound to port 26657.
//
// TODO: Add in a stabilization time parameter (i.e. a minimum number of peers
//       must be present when polled repeatedly for a period of time).
func waitForTendermintNetworkPeers(
	startingPeerAddrs []string,
	minPeers int,
	timeout time.Duration,
	logger logging.Logger,
) ([]string, error) {
	logger.Info(
		"Waiting for peers to connect",
		"minPeers", minPeers,
		"timeout", fmt.Sprintf("%.2f seconds", timeout.Seconds()),
	)

	startTime := time.Now()
	peers := make(map[string]*tendermintPeerInfo)
	for _, peerURL := range startingPeerAddrs {
		u, err := url.Parse(peerURL)
		if err != nil {
			return nil, fmt.Errorf("failed to parse peer URL %s: %s", peerURL, err)
		}
		peerAddr := fmt.Sprintf("http://%s:26657", u.Hostname())
		peers[peerAddr] = &tendermintPeerInfo{
			Addr:      peerAddr,
			Client:    client.NewHTTP(peerAddr, "/websocket"),
			PeerAddrs: make([]string, 0),
		}
	}
	for {
		remainingTimeout := timeout - time.Since(startTime)
		if remainingTimeout < 0 {
			return nil, fmt.Errorf("timed out waiting for Tendermint peer crawl to complete")
		}
		newPeers, err := getTendermintNetworkPeers(peers, remainingTimeout, logger)
		if err != nil {
			return nil, err
		}
		// we only care if we've discovered more peers than in the previous attempt
		if len(newPeers) > len(peers) {
			peers = newPeers
		}
		if len(peers) >= minPeers {
			logger.Info("All required peers connected", "count", len(peers))
			// we're done here
			return peerMapToList(peers, minPeers), nil
		} else {
			logger.Debug("Peers discovered so far", "count", len(peers), "peers", peers)
			time.Sleep(1 * time.Second)
		}
	}
}

// Queries the given peers (in parallel) to construct a unique set of known
// peers across the entire network.
func getTendermintNetworkPeers(
	peers map[string]*tendermintPeerInfo, // Any existing peers we know about already
	timeout time.Duration, // Maximum timeout for the entire operation
	logger logging.Logger,
) (map[string]*tendermintPeerInfo, error) {
	startTime := time.Now()
	peerInfoc := make(chan *tendermintPeerInfo, len(peers))
	errc := make(chan error, len(peers))
	logger.Debug("Querying peers for more peers", "count", len(peers))
	// parallelize querying all the Tendermint nodes' peers
	for _, peer := range peers {
		go func(peer_ *tendermintPeerInfo) {
			netInfo, err := peer_.Client.NetInfo()
			if err != nil {
				logger.Debug("Failed to query peer - skipping", "addr", peer_.Addr, "err", err)
				errc <- err
				return
			}
			peerAddrs := make([]string, 0)
			for _, peerInfo := range netInfo.Peers {
				peerAddrs = append(peerAddrs, fmt.Sprintf("http://%s:26657", peerInfo.RemoteIP))
			}
			peerInfoc <- &tendermintPeerInfo{
				Addr:      peer_.Addr,
				Client:    peer_.Client,
				PeerAddrs: peerAddrs,
			}
		}(peer)
	}
	result := make(map[string]*tendermintPeerInfo)
	expectedNetInfoResults := len(peers)
	receivedNetInfoResults := 0
	for {
		remainingTimeout := timeout - time.Since(startTime)
		if remainingTimeout < 0 {
			return nil, fmt.Errorf("timed out waiting for all peer network info to be returned")
		}
		select {
		case peerInfo := <-peerInfoc:
			result[peerInfo.Addr] = peerInfo
			receivedNetInfoResults++
		case <-errc:
			receivedNetInfoResults++
		case <-time.After(remainingTimeout):
			return nil, fmt.Errorf("timed out while waiting for all peer network info to be returned")
		}
		if receivedNetInfoResults >= expectedNetInfoResults {
			return resolveTendermintPeerMap(result), nil
		} else {
			// wait a little before polling  again
			time.Sleep(1 * time.Second)
		}
	}
}

func resolveTendermintPeerMap(peers map[string]*tendermintPeerInfo) map[string]*tendermintPeerInfo {
	result := make(map[string]*tendermintPeerInfo)
	for addr, peer := range peers {
		result[addr] = peer

		for _, peerAddr := range peer.PeerAddrs {
			if _, exists := result[peerAddr]; !exists {
				result[peerAddr] = &tendermintPeerInfo{
					Addr:      peerAddr,
					Client:    client.NewHTTP(peerAddr, "/websocket"),
					PeerAddrs: make([]string, 0),
				}
			}
		}
	}
	return result
}

func peerMapToList(peers map[string]*tendermintPeerInfo, maxCount int) []string {
	result := make([]string, 0)
	for _, peer := range peers {
		result = append(result, peer.Addr)
		if len(result) >= maxCount {
			break
		}
	}
	return result
}
