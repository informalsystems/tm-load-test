# Changelog

## v0.3.0
* [\#13](https://github.com/interchainio/tm-load-test/pull/13) - Add basic HTTP
  authentication to `tm-outage-sim-server` utility.
* [\#12](https://github.com/interchainio/tm-load-test/pull/12) - Add standalone
  mode to be able to run load testing tool locally in a simple way.
* [\#11](https://github.com/interchainio/tm-load-test/pull/11) - Allow for basic
  HTTP authentication between the master and slave nodes for additional
  security.
* [\#10](https://github.com/interchainio/tm-load-test/pull/10) - Allow clients
  to continuously send transactions until the maximum time limit is reached
  without having a limit imposed on the number of transactions.

## v0.2.0
* First alpha release.
* Refactored comms mechanism between master and slaves to use HTTP for more
  robust communication and to simplify.
* Added the WebSockets-based client for interacting with Tendermint nodes over
  WebSockets (`kvstore-websockets`).

## v0.1.0
* Initial release to configure release management.

