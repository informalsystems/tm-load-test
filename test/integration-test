#!/usr/bin/env bash
#
# This script is intended to be executed from the root of the tm-load-test
# repository.

make localnet-stop || exit 1
rm -rf build
make localnet-start || exit 1
go test -count 1 -v --tags=integration -mod=readonly -timeout 8m -coverprofile=coverage.txt -covermode=atomic ./...
TEST_EXIT_CODE=$?
make localnet-stop

exit ${TEST_EXIT_CODE}
