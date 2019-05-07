# Example Configuration Files

Each TOML file in this folder provides an example as to how the load testing
tool can be configured. The following configurations are available at present:

* `load-test.toml` - Executes using the `kvstore-http` client type with 2 slave
  nodes against a Tendermint node running on localhost. Interacts with the node
  via the HTTP RPC interface.
* `load-test-websockets.toml` - Executes the `kvstore-websockets` client type
  with 2 slave nodes against a Tendermint node running on localhost. Interacts
  with the node via the WebSockets RPC interface.
* `load-test-autodetect-peers.toml` - Executes the `kvstore-http` client type
  with 2 slave nodes against a Tendermint node running on localhost. Interacts
  with the node via the HTTP RPC interface. Attempts to glean a list of
  Tendermint node targets from the Tendermint node running on localhost through
  its `net_info` RPC endpoint.
