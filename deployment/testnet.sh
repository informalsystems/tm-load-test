#!/bin/bash
set -e

BCRYPT_APP=${BCRYPT_APP:-"bcrypt-me gen -b"}
TENDERMINT_SRC=${TENDERMINT_SRC:-"${GOPATH}/src/github.com/tendermint/tendermint"}
LOADTEST_SRC=${LOADTEST_SRC:-".."}
NOBUILD=${NOBUILD:-"0"}
INSTALL_TENDERMINT=${INSTALL_TENDERMINT:-"0"}
INVENTORY=${INVENTORY:-"hosts"}
DEPLOY_OUTAGE_SIM=${DEPLOY_OUTAGE_SIM:-"1"}
GEN_TESTNET_CONFIG=${GEN_TESTNET_CONFIG:-"1"}
DEPLOY_TENDERMINT=${DEPLOY_TENDERMINT:-"1"}

OUTAGE_SIM_USER=${OUTAGE_SIM_USER:-"loadtest"}
OUTAGE_SIM_DEFAULT_PASSWORD=`hexdump -n 16 -e '4/4 "%08x" 1 "\n"' /dev/urandom`
OUTAGE_SIM_PASSWORD=${OUTAGE_SIM_PASSWORD:-${OUTAGE_SIM_DEFAULT_PASSWORD}}
OUTAGE_SIM_PASSWORD_HASH=${OUTAGE_SIM_PASSWORD_HASH:-""}
OUTAGE_SIM_PORT=${OUTAGE_SIM_PORT:-26680}

# Try to use the bcrypt application to generate a hash of the password
if [ "${OUTAGE_SIM_PASSWORD_HASH}" == "" ]; then
    OUTAGE_SIM_PASSWORD_HASH=`${BCRYPT_APP} ${OUTAGE_SIM_PASSWORD}`
fi

TM_CONFIG_TEMPLATE=${TM_CONFIG_TEMPLATE:-"./default-tendermint-config.toml"}
TM_VALIDATORS=${TM_VALIDATORS:-4}
TM_NON_VALIDATORS=${TM_NON_VALIDATORS:-0}
TM_HOSTNAME_PREFIX=${TM_HOSTNAME_PREFIX:-node}
TM_HOSTNAME_SUFFIX=${TM_HOSTNAME_SUFFIX:-""}
TM_COPY_CONFIG=${TM_COPY_CONFIG:-"1"}
TM_USER=${TM_USER:-tendermint}
TM_GROUP=${TM_GROUP:-tendermint}

ANSIBLE_OPTS="-i ${INVENTORY}"
ANSIBLE_USER=${ANSIBLE_USER:-""}
ANSIBLE_USER_OPTS=""

if [ "${ANSIBLE_USER}" != "" ]; then
    ANSIBLE_USER_OPTS=" -u ${ANSIBLE_USER}"
fi

ANSIBLE_OPTS="${ANSIBLE_OPTS}${ANSIBLE_USER_OPTS}"
LOG=""
LF=$'\n'

# For if we need a local development version of Tendermint installed locally
# (perhaps it's got some additional new code for testnet generation, for
# example).
if [ "${INSTALL_TENDERMINT}" == 1 ]; then
    echo "-------------------------------------------------------------------------------"
    echo "Installing Tendermint locally"
    echo "-------------------------------------------------------------------------------"
    make -C ${TENDERMINT_SRC} install
    echo ""
    echo ""
    LOG="${LOG}* Installed Tendermint locally${LF}"
fi

# By default we build the tm-outage-sim-server and tendermint binaries for
# Linux, because we assume we're going to be deploying them to Linux machines.
if [ "${NOBUILD}" == 0 ]; then
    echo "-------------------------------------------------------------------------------"
    echo "Building tm-outage-sim-server"
    echo "-------------------------------------------------------------------------------"
    make -C ${LOADTEST_SRC} build-tm-outage-sim-server-linux
    echo ""
    echo ""
    LOG="${LOG}* Built tm-outage-sim-server${LF}"

    echo "-------------------------------------------------------------------------------"
    echo "Building Tendermint"
    echo "-------------------------------------------------------------------------------"
    make -C ${TENDERMINT_SRC} build-linux
    echo ""
    echo ""
    LOG="${LOG}* Build Linux version of Tendermint${LF}"
fi

# By default we deploy an outage simulator service alongside every Tendermint
# node so that the load testing tool can control whether each Tendermint node is
# to be turned on/off during testing.
if [ "${DEPLOY_OUTAGE_SIM}" == 1 ]; then
    echo "-------------------------------------------------------------------------------"
    echo "Deploying outage simulator"
    echo "-------------------------------------------------------------------------------"

    echo "outage_sim_binary: ${LOADTEST_SRC}/build/tm-outage-sim-server
outage_sim_port: ${OUTAGE_SIM_PORT}
outage_sim_user: ${OUTAGE_SIM_USER}
outage_sim_password_hash: \"${OUTAGE_SIM_PASSWORD_HASH}\"
" > /tmp/outage-sim-extra-vars.yaml

    ansible-playbook ${ANSIBLE_OPTS} \
        -e "@/tmp/outage-sim-extra-vars.yaml" \
        deploy-outage-sim.yaml

    echo ""
    echo ""
    rm /tmp/outage-sim-extra-vars.yaml
    LOG="${LOG}* Deployed outage simulator with username:password = ${OUTAGE_SIM_USER}:${OUTAGE_SIM_PASSWORD}${LF}"
fi

# By default we deploy Tendermint to the target nodes.
if [ "${DEPLOY_TENDERMINT}" == 1 ]; then
    echo "-------------------------------------------------------------------------------"
    echo "Deploying Tendermint nodes"
    echo "-------------------------------------------------------------------------------"

    # First we (optionally) generate the test network configuration
    if [ "${GEN_TESTNET_CONFIG}" == 1 ]; then
        TM_TESTNET_CONFIG=""
        if [ "${TM_CONFIG_TEMPLATE}" != "" ]; then
            TM_TESTNET_CONFIG=" --config ${TM_CONFIG_TEMPLATE}"
        fi
        tendermint testnet${TM_TESTNET_CONFIG} \
            --node-dir-prefix ${TM_HOSTNAME_PREFIX} \
            --hostname-prefix ${TM_HOSTNAME_PREFIX} \
            --hostname-suffix ${TM_HOSTNAME_SUFFIX} \
            --v ${TM_VALIDATORS} \
            --n ${TM_NON_VALIDATORS} \
            --o /tmp/testnet
    fi

    # Second, deploy the test network with Ansible
    echo "tendermint_binary: ${TENDERMINT_SRC}/build/tendermint
tendermint_config_path: /tmp/testnet
copy_tendermint_config: ${TM_COPY_CONFIG}
tendermint_user: ${TM_USER}
tendermint_group: ${TM_GROUP}
" > /tmp/tendermint-extra-vars.yaml
    ansible-playbook ${ANSIBLE_OPTS} \
        -e "@/tmp/tendermint-extra-vars.yaml" \
        deploy-tendermint.yaml

    echo ""
    echo ""
    
    # Cleanup
    rm /tmp/tendermint-extra-vars.yaml
    rm -rf /tmp/testnet

    LOG="${LOG}* Deployed Tendermint network${LF}"
fi

echo "-------------------------------------------------------------------------------"
echo "Summary"
echo "-------------------------------------------------------------------------------"
echo "${LOG}"
