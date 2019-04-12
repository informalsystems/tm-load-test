#!/bin/bash
set -e

if [ "${FAST_MODE}" == "false" ] && [ "${FETCH_RESULTS_ONLY}" == "false" ]; then
    echo "Building tm-load-test for Linux..."
    make -C ${TMLOADTEST_PATH} build-tm-load-test-linux
fi
