# Changelog

## v0.2.1
* [\#10](https://github.com/interchainio/tm-load-test/pull/10) Allow clients
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

