#!/bin/bash
set -e

echo "Building executables for load testing..."
if [ "${FAST_MODE}" == "false" ]; then
    if [ "${WITH_CLEVELDB}" == "true" ]; then
        if [ "${UNAME_S}" != "Linux" ]; then
            echo "ERROR: The cleveldb support can only be built from a Linux machine (yours is \"${UNAME_S}\")"
            exit 1
        fi
        go get -u github.com/jmhodges/levigo
        echo "Building Tendermint with cleveldb support..."
        if [ "${INSTALL_TENDERMINT_LOCALLY}" == "true" ]; then
            CGO_LDFLAGS="-lsnappy" make -C ${TENDERMINT_SRC} install_c
        fi
        CGO_LDFLAGS="-lsnappy" make -C ${TENDERMINT_SRC} build_c
    else
        echo "Building Tendermint..."
        if [ "${INSTALL_TENDERMINT_LOCALLY}" == "true" ]; then
            make -C ${TENDERMINT_SRC} install
        fi
        make -C ${TENDERMINT_SRC} build-linux
    fi
    echo "Building load testing tools..."
    make -C ${TM_NETWORKS_SRC} tools-linux
fi
