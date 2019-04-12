# Distributed `kvstore` Load Test

This scenario is similar to
[`002-kvstore-loadtest`](../002-kvstore-loadtest/README.md) in that it attempts
to generate a reasonable amount of load by hitting the HTTP interface of the
deployed Tendermint nodes, assuming that the `kvstore` proxy app is running. It
does so, however, from multiple source machines.

## Execution
To execute the load test from a single machine:

```bash
# Optional: deploy a clean reference network
make deploy:001-reference

# Standard load test
make scenario:003-kvstore-loadtest-distributed
```

## Statistics
After load test execution, the stdout output, log file and CSV statistics files
for each machine will be fetched and placed into the
`/tmp/003-kvstore-loadtest-distributed/{node_id}` folders.
