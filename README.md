[![CircleCI](https://circleci.com/gh/informalsystems/tm-load-test/tree/master.svg?style=svg)](https://circleci.com/gh/informalsystems/tm-load-test/tree/master)

# tm-load-test

`tm-load-test` is a distributed load testing tool (and framework) for load
testing [Tendermint](https://tendermint.com/) networks and aims to effectively
be the successor to [`tm-bench`](https://github.com/tendermint/tendermint/tree/master/tools/tm-bench).

Naturally, any  transactions sent to a Tendermint network are specific to the
ABCI application running on that network. As such, the `tm-load-test` tool comes
with built-in support for the `kvstore` ABCI application, but you can
[build your own clients](./pkg/loadtest/README.md) for your own apps.

**NB: `tm-load-test` is currently alpha-quality software. Semantic versioning is
not strictly adhered to prior to a v1.0 release, so breaking API changes can
emerge with minor version releases.**

## Requirements
In order to build and use the tools, you will need:

* Go 1.13+
* `make`

## Building
To build the `tm-load-test` binary in the `build` directory:

```bash
make
```

## Usage
`tm-load-test` can be executed in one of two modes: **standalone**, or
**master/slave**.

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

### Master/Slave Mode
In master/slave mode, which is best used for large-scale, distributed load 
testing, `tm-load-test` allows you to have multiple slave machines connect to
a single master to obtain their configuration and coordinate their operation.

The master acts as a simple WebSockets host, and the slaves are WebSockets
clients.

On the master machine:

```bash
# Run tm-load-test with similar parameters to the standalone mode, but now 
# specifying the number of slaves to expect (--expect-slaves) and the host:port
# to which to bind (--bind) and listen for incoming slave requests.
tm-load-test \
    master \
    --expect-slaves 2 \
    --bind localhost:26670 \
    -c 1 -T 10 -r 1000 -s 250 \
    --broadcast-tx-method async \
    --endpoints ws://tm-endpoint1.somewhere.com:26657/websocket,ws://tm-endpoint2.somewhere.com:26657/websocket
```

On each slave machine:

```bash
# Just tell the slave where to find the master - it will figure out the rest.
tm-load-test slave --master localhost:26680
```

For more help, see the command line parameters' descriptions:

```bash
tm-load-test master --help
tm-load-test slave --help
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
`--expect-peers` parameter is supplied and is non-zero. The default behaviour
if `--expect-peers` is not supplied is effectively the `supplied` endpoint
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
As of v0.4.1, `tm-load-test` exposes a number of metrics when in master/slave 
mode, but only from the master's web server at the `/metrics` endpoint. So if
you bind your master node to `localhost:26670`, you should be able to get these
metrics from:

```bash
curl http://localhost:26670/metrics
```

The following kinds of metrics are made available here:

* Total number of transactions recorded from the master's perspective (across
  all slaves)
* Total number of transactions sent by each slave
* The status of the master node, which is a gauge that indicates one of the 
  following codes:
  * 0 = Master starting
  * 1 = Master waiting for all peers to connect
  * 2 = Master waiting for all slaves to connect
  * 3 = Load test underway
  * 4 = Master and/or one or more slave(s) failed
  * 5 = All slaves completed load testing successfully
* The status of each slave node, which is also a gauge that indicates one of the
  following codes:
  * 0 = Slave connected
  * 1 = Slave accepted
  * 2 = Slave rejected
  * 3 = Load testing underway
  * 4 = Slave failed
  * 5 = Slave completed load testing successfully
* Standard Prometheus-provided metrics about the garbage collector in 
  `tm-load-test`
* The ID of the load test currently underway (defaults to 0), set by way of the
  `--load-test-id` flag on the master

## Aggregate Statistics
As of `tm-load-test` v0.7.0, one can now write simple aggregate statistics to
a CSV file once testing completes by specifying the `--stats-output` flag:

```bash
# In standalone mode
tm-load-test -c 1 -T 10 -r 1000 -s 250 \
    --broadcast-tx-method async \
    --endpoints ws://tm-endpoint1.somewhere.com:26657/websocket,ws://tm-endpoint2.somewhere.com:26657/websocket \
    --stats-output /path/to/save/stats.csv

# From the master in master/slave mode
tm-load-test \
    master \
    --expect-slaves 2 \
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

