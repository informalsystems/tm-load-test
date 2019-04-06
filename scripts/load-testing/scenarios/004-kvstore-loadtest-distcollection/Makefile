INVENTORY?=../../inventory/hosts

# Which test network should be deployed, if this script deploys one
# automatically?
TEST_NETWORK?=001-reference
NETWORK_CONFIG_SCRIPT?=./scripts/001-default.sh
NETWORK_VALIDATORS?=4

# If we're deploying the test network, do we deploy with cleveldb support?
WITH_CLEVELDB?=false

# Should the test network be redeployed before each test?
DEPLOY_NETWORK_BEFORE_TEST?=yes

# Should each load test be executed in fast mode?
FAST_MODE?=false

# Should output logging happen at DEBUG-level?
DEBUG_MODE?=false

# The number of load tests to execute
TEST_COUNT?=10

# Which load testing client to use
CLIENTS_TYPE?=tmrpc

# Number of clients to spawn
CLIENTS_SPAWN_START?=100
CLIENTS_SPAWN_END?=1000

# The rate at which to spawn clients on each slave node
CLIENTS_SPAWN_RATE_START?=10
CLIENTS_SPAWN_RATE_END?=100

# The maximum number of interactions per client before considering a load test
# to have completed
CLIENTS_MAX_INTERACTIONS?=100

# Request wait minimum times (in milliseconds)
CLIENTS_REQUEST_WAIT_MIN_START?=1000
CLIENTS_REQUEST_WAIT_MIN_END?=1000

# Request wait maximum times (in milliseconds)
CLIENTS_REQUEST_WAIT_MAX_START?=1000
CLIENTS_REQUEST_WAIT_MAX_END?=1000

# Where do we store the results we fetch from the load test servers?
LOCAL_RESULTS_DIR?=/tmp/004-kvstore-loadtest-distcollection
# Where do we store the load test collection executor logs?
LOADTEST_LOG?=$(LOCAL_RESULTS_DIR)/collection.log
.PHONY: execute

execute:
	INVENTORY=$(INVENTORY) \
		TEST_NETWORK=$(TEST_NETWORK) \
		NETWORK_CONFIG_SCRIPT=$(NETWORK_CONFIG_SCRIPT) \
		NETWORK_VALIDATORS=$(NETWORK_VALIDATORS) \
		DEPLOY_NETWORK_BEFORE_TEST=$(DEPLOY_NETWORK_BEFORE_TEST) \
		WITH_CLEVELDB=$(WITH_CLEVELDB) \
		FAST_MODE=$(FAST_MODE) \
		DEBUG_MODE=$(DEBUG_MODE) \
		TEST_COUNT=$(TEST_COUNT) \
		CLIENTS_TYPE=$(CLIENTS_TYPE) \
		CLIENTS_SPAWN_START=$(CLIENTS_SPAWN_START) \
		CLIENTS_SPAWN_END=$(CLIENTS_SPAWN_END) \
		CLIENTS_SPAWN_RATE_START=$(CLIENTS_SPAWN_RATE_START) \
		CLIENTS_SPAWN_RATE_END=$(CLIENTS_SPAWN_RATE_END) \
		CLIENTS_MAX_INTERACTIONS=$(CLIENTS_MAX_INTERACTIONS) \
		CLIENTS_REQUEST_WAIT_MIN_START=$(CLIENTS_REQUEST_WAIT_MIN_START) \
		CLIENTS_REQUEST_WAIT_MIN_END=$(CLIENTS_REQUEST_WAIT_MIN_END) \
		CLIENTS_REQUEST_WAIT_MAX_START=$(CLIENTS_REQUEST_WAIT_MAX_START) \
		CLIENTS_REQUEST_WAIT_MAX_END=$(CLIENTS_REQUEST_WAIT_MAX_END) \
		LOCAL_RESULTS_DIR=$(LOCAL_RESULTS_DIR) \
		LOADTEST_LOG=$(LOADTEST_LOG) \
		./scripts/execute.sh
