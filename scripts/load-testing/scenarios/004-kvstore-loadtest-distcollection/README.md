# Distributed `kvstore` Load Test Collection Executor

This scenario allows one to automatically execute multiple distributed load
tests (`003-kvstore-loadtest-distributed`) while varying the `CLIENTS_SPAWN`,
`CLIENTS_SPAWN_RATE`, `CLIENTS_REQUEST_WAIT_MIN`, and `CLIENTS_REQUEST_WAIT_MAX`
parameters, as well as whether or not to redeploy the relevant Tendermint
network between each load test.
