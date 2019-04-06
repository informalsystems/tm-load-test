#!/bin/bash
# convert the Python "True" or "False" to "true" or "false"
DEBUG_MODE=`echo ${DEBUG_MODE} | awk '{print tolower($0)}'`

TEST_COUNT_MINUS_ONE=$(expr ${TEST_COUNT} - 1)
CLIENTS_SPAWN_RANGE=$(expr ${CLIENTS_SPAWN_END} - ${CLIENTS_SPAWN_START})
CLIENTS_SPAWN_INC=$(expr ${CLIENTS_SPAWN_RANGE} / ${TEST_COUNT_MINUS_ONE})

CLIENTS_SPAWN_RATE_RANGE=$(expr ${CLIENTS_SPAWN_RATE_END} - ${CLIENTS_SPAWN_RATE_START})
CLIENTS_SPAWN_RATE_INC=$(expr ${CLIENTS_SPAWN_RATE_RANGE} / ${TEST_COUNT_MINUS_ONE})

CLIENTS_REQUEST_WAIT_MIN_RANGE=$(expr ${CLIENTS_REQUEST_WAIT_MIN_END} - ${CLIENTS_REQUEST_WAIT_MIN_START})
CLIENTS_REQUEST_WAIT_MIN_INC=$(expr ${CLIENTS_REQUEST_WAIT_MIN_RANGE} / ${TEST_COUNT_MINUS_ONE})
CLIENTS_REQUEST_WAIT_MAX_RANGE=$(expr ${CLIENTS_REQUEST_WAIT_MAX_END} - ${CLIENTS_REQUEST_WAIT_MAX_START})
CLIENTS_REQUEST_WAIT_MAX_INC=$(expr ${CLIENTS_REQUEST_WAIT_MAX_RANGE} / ${TEST_COUNT_MINUS_ONE})

# make sure the relevant output folders exist
rm -rf ${LOCAL_RESULTS_DIR}
mkdir -p ${LOCAL_RESULTS_DIR}
mkdir -p $(dirname ${LOADTEST_LOG})

# truncate the load test log file
cat /dev/null > ${LOADTEST_LOG}

function loadtest_log {
    echo ""
    echo "---------------------------------------------------------------------"
    echo "$1"
    echo "---------------------------------------------------------------------"
    echo ""
    echo "$1" >> ${LOADTEST_LOG}
}

function cur_test_params {
    cur_test=$1
    clients_spawn=$2
    clients_spawn_rate=$3
    clients_request_wait_min=$4
    clients_request_wait_max=$5

    echo ""
    echo "cur_test=${cur_test}"
    echo "FAST_MODE=${FAST_MODE}"
    echo "CLIENTS_TYPE=${CLIENTS_TYPE}"
    echo "CLIENTS_SPAWN=${clients_spawn}"
    echo "CLIENTS_SPAWN_RATE=${clients_spawn_rate}.0"
    echo "CLIENTS_REQUEST_WAIT_MIN=${clients_request_wait_min}ms"
    echo "CLIENTS_REQUEST_WAIT_MAX=${clients_request_wait_max}ms"
    echo "CLIENTS_MAX_INTERACTIONS=${CLIENTS_MAX_INTERACTIONS}"
    echo "LOCAL_RESULTS_DIR=${LOCAL_RESULTS_DIR}/test${cur_test}"
    echo ""
}

function global_test_params {
    echo ""
    echo "TEST_NETWORK=${TEST_NETWORK}"
    echo "NETWORK_CONFIG_SCRIPT=${NETWORK_CONFIG_SCRIPT}"
    echo "NETWORK_VALIDATORS=${NETWORK_VALIDATORS}"
    echo "DEPLOY_NETWORK_BEFORE_TEST=${DEPLOY_NETWORK_BEFORE_TEST}"
    echo "FAST_MODE=${FAST_MODE}"
    echo "TEST_COUNT=${TEST_COUNT}"
    echo "CLIENTS_TYPE=${CLIENTS_TYPE}"
    echo "CLIENTS_SPAWN_START=${CLIENTS_SPAWN_START}"
    echo "CLIENTS_SPAWN_END=${CLIENTS_SPAWN_END}"
    echo "CLIENTS_SPAWN_RATE_START=${CLIENTS_SPAWN_RATE_START}"
    echo "CLIENTS_SPAWN_RATE_END=${CLIENTS_SPAWN_RATE_END}"
    echo "CLIENTS_MAX_INTERACTIONS=${CLIENTS_MAX_INTERACTIONS}"
    echo "CLIENTS_REQUEST_WAIT_MIN_START=${CLIENTS_REQUEST_WAIT_MIN_START}"
    echo "CLIENTS_REQUEST_WAIT_MIN_END=${CLIENTS_REQUEST_WAIT_MIN_END}"
    echo "CLIENTS_REQUEST_WAIT_MAX_START=${CLIENTS_REQUEST_WAIT_MAX_START}"
    echo "CLIENTS_REQUEST_WAIT_MAX_END=${CLIENTS_REQUEST_WAIT_MAX_END}"
    echo "LOCAL_RESULTS_DIR=${LOCAL_RESULTS_DIR}"
    echo "LOADTEST_LOG=${LOADTEST_LOG}"
    echo "---------------------"
    echo "CLIENTS_SPAWN_INC=${CLIENTS_SPAWN_INC}"
    echo "CLIENTS_SPAWN_RATE_INC=${CLIENTS_SPAWN_RATE_INC}"
    echo "CLIENTS_REQUEST_WAIT_MIN_INC=${CLIENTS_REQUEST_WAIT_MIN_INC}"
    echo "CLIENTS_REQUEST_WAIT_MAX_INC=${CLIENTS_REQUEST_WAIT_MAX_INC}"
    echo ""
}

GLOBAL_TEST_PARAMS="$(global_test_params)"
echo "${GLOBAL_TEST_PARAMS}"
echo "${GLOBAL_TEST_PARAMS}" > ${LOCAL_RESULTS_DIR}/global_test_params

clients_spawn=${CLIENTS_SPAWN_START}
clients_spawn_rate=${CLIENTS_SPAWN_RATE_START}
clients_request_wait_min=${CLIENTS_REQUEST_WAIT_MIN_START}
clients_request_wait_max=${CLIENTS_REQUEST_WAIT_MAX_START}
cur_test=0

while [ ${cur_test} -lt ${TEST_COUNT} ]; do
    CUR_TEST_PARAMS="$(cur_test_params ${cur_test} ${clients_spawn} ${clients_spawn_rate} ${clients_request_wait_min} ${clients_request_wait_max})"
    loadtest_log "${CUR_TEST_PARAMS}"
    TEST_OUTPUT_DIR=${LOCAL_RESULTS_DIR}/test${cur_test}

    if [ "${DEPLOY_NETWORK_BEFORE_TEST}" == "yes" ]; then
        FAST_MODE=${FAST_MODE} \
            make -C ../../networks/${TEST_NETWORK} deploy
        EXIT_CODE=$?
        if [ ${EXIT_CODE} != 0 ]; then
            loadtest_log "FAILED to redeploy network prior to test. Exit code ${EXIT_CODE}."
            exit ${EXIT_CODE}
        fi
    fi

    # execute the actual load test
    # NOTE: CLIENTS_SPAWN_RATE must be a floating point value
    # TODO: Fix this in the deploy.yml script
    INVENTORY=${INVENTORY} \
        FAST_MODE=${FAST_MODE} \
        NETWORK_CONFIG_SCRIPT=${NETWORK_CONFIG_SCRIPT} \
        NETWORK_VALIDATORS=${NETWORK_VALIDATORS} \
        WITH_CLEVELDB=${WITH_CLEVELDB} \
        DEBUG_MODE=${DEBUG_MODE} \
        CLIENTS_TYPE=${CLIENTS_TYPE} \
        CLIENTS_SPAWN=${clients_spawn} \
        CLIENTS_SPAWN_RATE="${clients_spawn_rate}.0" \
        CLIENTS_REQUEST_WAIT_MIN=${clients_request_wait_min}ms \
        CLIENTS_REQUEST_WAIT_MAX=${clients_request_wait_max}ms \
        CLIENTS_MAX_INTERACTIONS=${CLIENTS_MAX_INTERACTIONS} \
        LOCAL_RESULTS_DIR=${TEST_OUTPUT_DIR} \
        make -C ../003-kvstore-loadtest-distributed execute
    EXIT_CODE=$?
    if [ ${EXIT_CODE} != 0 ]; then
        loadtest_log "FAILED to execute load test ${cur_test}. Exit code ${EXIT_CODE}."
        exit ${EXIT_CODE}
    fi

    # do this after the test, or else 003-kvstore-loadtest-distributed will
    # delete all the files in the folder prior to the test
    echo "${CUR_TEST_PARAMS}" > ${TEST_OUTPUT_DIR}/test_params

    clients_spawn=$(expr ${clients_spawn} + ${CLIENTS_SPAWN_INC})
    clients_spawn_rate=$(expr ${clients_spawn_rate} + ${CLIENTS_SPAWN_RATE_INC})
    clients_request_wait_min=$(expr ${clients_request_wait_min} + ${CLIENTS_REQUEST_WAIT_MIN_INC})
    clients_request_wait_max=$(expr ${clients_request_wait_max} + ${CLIENTS_REQUEST_WAIT_MAX_INC})
    cur_test=$(expr ${cur_test} + 1)

    # we only allow FAST_MODE to be false for a single iteration
    FAST_MODE=true
done

loadtest_log "LOAD TESTING COMPLETE"
