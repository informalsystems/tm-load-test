#!/bin/bash
set -e

if [ -z "$1" ]; then
    echo "Usage: ./examples/run.sh <config-file>"
    echo "NOTE: By default, this script assumes you only have 1 master with 2 slaves."
    exit 1
fi

make build
./build/tm-load-test -c $1 -mode slave -v > tm-load-test.slave1.log 2>&1 &
SLAVE1_PID=$!
echo "Started slave 1 with PID ${SLAVE1_PID}"

./build/tm-load-test -c $1 -mode slave -v > tm-load-test.slave2.log 2>&1 &
SLAVE2_PID=$!
echo "Started slave 2 with PID ${SLAVE2_PID}"

# Start the master in the foreground to be able to watch/kill the process
./build/tm-load-test -c $1 -mode master -v

