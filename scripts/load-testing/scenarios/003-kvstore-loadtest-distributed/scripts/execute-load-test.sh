#!/bin/bash
set -e

# convert the Python "True" or "False" to "true" or "false"
DEBUG_MODE=`echo ${DEBUG_MODE} | awk '{print tolower($0)}'`
VERBOSE=""
if [ "${DEBUG_MODE}" == "true" ]; then
    VERBOSE="-v"
fi

if [ "${INVENTORY_HOSTNAME}" == "${MASTER_NODE}" ]; then
    tm-load-test -c config.toml -master ${VERBOSE}
else
    tm-load-test -c config.toml -slave ${VERBOSE}
fi
