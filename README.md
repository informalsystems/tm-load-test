# tm-load-test

`tm-load-test` is a distributed load testing tool (and framework) for load
testing [Tendermint](https://tendermint.com/) networks and aims to effectively
be the successor to [`tm-bench`](https://github.com/tendermint/tendermint/tree/v0.32.x/tools/tm-bench).

Naturally, any  transactions sent to a Tendermint network are specific to the
ABCI application running on that network. As such, the `tm-load-test` tool comes
with built-in support for the `kvstore` ABCI application, but you can [build
your own clients](./pkg/loadtest/README.md) for your own apps.

## Requirements

`tm-load-test` is currently tested using Go v1.20.

## Building

To build the `tm-load-test` binary in the `build` directory:

```bash
make
```

## Usage

`tm-load-test` can be executed in one of two modes: **standalone**, or
**coordinator/worker**.

### Standalone Mode

In standalone mode, `tm-load-test` operates in a similar way to `tm-bench`:

```bash
tm-load-test -c 1 -T 10 -r 1000 -s 250 \
    --broadcast-tx-method async \
    --endpoints ws://tm-endpoint1.somewhere.com:26657/websocket,ws://tm-endpoint2.somewhere.com:26657/websocket
```

To see a description of what all of the parameters mean, simply run:

```bash
tm-load-test --help
```

### Coordinator/Worker Mode

In coordinator/worker mode, which is best used for large-scale, distributed load
testing, `tm-load-test` allows you to have multiple worker machines connect to a
single coordinator to obtain their configuration and coordinate their operation.

The coordinator acts as a simple WebSockets host, and the workers are WebSockets
clients.

On the coordinator machine:

```bash
# Run tm-load-test with similar parameters to the standalone mode, but now
# specifying the number of workers to expect (--expect-workers) and the host:port
# to which to bind (--bind) and listen for incoming worker requests.
tm-load-test \
    coordinator \
    --expect-workers 2 \
    --bind localhost:26670 \
    -c 1 -T 10 -r 1000 -s 250 \
    --broadcast-tx-method async \
    --endpoints ws://tm-endpoint1.somewhere.com:26657/websocket,ws://tm-endpoint2.somewhere.com:26657/websocket
```

On each worker machine:

```bash
# Just tell the worker where to find the coordinator - it will figure out the rest.
tm-load-test worker --coordinator localhost:26680
```

For more help, see the command line parameters' descriptions:

```bash
tm-load-test coordinator --help
tm-load-test worker --help
```

### Endpoint Selection Strategies

As of v0.5.1, an endpoint selection strategy can now be given to `tm-load-test`
as a parameter (`--endpoint-select-method`) to control the way in which
endpoints are selected for load testing. There are several options:

1. `supplied` (the default) - only use the supplied endpoints (via the
   `--endpoints` parameter) to submit transactions.
2. `discovered` - only use endpoints discovered through the supplied endpoints
   (by way of crawling the Tendermint peers' network info), but do not use any
   of the supplied endpoints.
3. `any` - use both the supplied and discovered endpoints to perform load
   testing.

**NOTE**: These selection strategies only apply if, and only if, the
`--expect-peers` parameter is supplied and is non-zero. The default behaviour if
`--expect-peers` is not supplied is effectively the `supplied` endpoint
selection strategy.

### Minimum Peer Connectivity

As of v0.6.0, `tm-load-test` can now wait for a minimum level of P2P
connectivity before starting the load testing. By using the
`--min-peer-connectivity` command line switch, along with `--expect-peers`, one
can restrict this.

What this does under the hood is that it checks how many peers are in each
queried peer's address book, and for all reachable peers it checks what the
minimum address book size is. Once the minimum address book size reaches the
configured value, the load testing can begin.

### Customizing

To implement your own client type to load test your own Tendermint ABCI
application, see the [`loadtest` package docs here](./pkg/loadtest/README.md).

## Monitoring

As of v0.4.1, `tm-load-test` exposes a number of metrics when in coordinator/worker
mode, but only from the coordinator's web server at the `/metrics` endpoint. So if
you bind your coordinator node to `localhost:26670`, you should be able to get these
metrics from:

```bash
curl http://localhost:26670/metrics
```

The following kinds of metrics are made available here:

* Total number of transactions recorded from the coordinator's perspective
  (across all workers)
* Total number of transactions sent by each worker
* The status of the coordinator node, which is a gauge that indicates one of the
  following codes:
  * 0 = Coordinator starting
  * 1 = Coordinator waiting for all peers to connect
  * 2 = Coordinator waiting for all workers to connect
  * 3 = Load test underway
  * 4 = Coordinator and/or one or more worker(s) failed
  * 5 = All workers completed load testing successfully
* The status of each worker node, which is also a gauge that indicates one of
  the following codes:
  * 0 = Worker connected
  * 1 = Worker accepted
  * 2 = Worker rejected
  * 3 = Load testing underway
  * 4 = Worker failed
  * 5 = Worker completed load testing successfully
* Standard Prometheus-provided metrics about the garbage collector in
  `tm-load-test`
* The ID of the load test currently underway (defaults to 0), set by way of the
  `--load-test-id` flag on the coordinator

## Aggregate Statistics

As of `tm-load-test` v0.7.0, one can now write simple aggregate statistics to a
CSV file once testing completes by specifying the `--stats-output` flag:

```bash
# In standalone mode
tm-load-test -c 1 -T 10 -r 1000 -s 250 \
    --broadcast-tx-method async \
    --endpoints ws://tm-endpoint1.somewhere.com:26657/websocket,ws://tm-endpoint2.somewhere.com:26657/websocket \
    --stats-output /path/to/save/stats.csv

# From the coordinator in coordinator/worker mode
tm-load-test \
    coordinator \
    --expect-workers 2 \
    --bind localhost:26670 \
    -c 1 -T 10 -r 1000 -s 250 \
    --broadcast-tx-method async \
    --endpoints ws://tm-endpoint1.somewhere.com:26657/websocket,ws://tm-endpoint2.somewhere.com:26657/websocket \
    --stats-output /path/to/save/stats.csv
```

The output CSV file has the following format at present:

```csv
Parameter,Value,Units
total_time,10.002,seconds
total_txs,9000,count
avg_tx_rate,899.818398,transactions per second
```

## Development

To run the linter and the tests:

```bash
make lint
make test
```

### Integration Testing

Integration testing requires Docker to be installed locally.

```bash
make integration-test
```

This integration test:

1. Sets up a 4-validator, fully connected Tendermint Core-based network on a
   192.168.0.0/16 subnet (the same kind of testnet as the Tendermint Core
   localnet).
2. Executes integration tests against the network in series (it's important that
   integration tests be executed in series so as to not overlap with one
   another).
3. Tears down the 4-validator network, reporting code coverage.

