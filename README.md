[![CircleCI](https://circleci.com/gh/interchainio/tm-load-test/tree/master.svg?style=svg)](https://circleci.com/gh/interchainio/tm-load-test/tree/master)

# tm-load-test

`tm-load-test` is a distributed load testing tool (and framework) for load
testing [Tendermint](https://tendermint.com/) networks and aims to effectively
be the successor to [`tm-bench`](https://github.com/tendermint/tendermint/tree/master/tools/tm-bench).

Naturally, any  transactions sent to a Tendermint network are specific to the
ABCI application running on that network. As such, the `tm-load-test` tool comes
with built-in support for the `kvstore` ABCI application, but you can
[build your own clients](./pkg/loadtest/README.md) for your own apps.

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

## Development
To run the linter and the tests:

```bash
make lint
make test
```

