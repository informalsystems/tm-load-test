INVENTORY?=../../inventory/hosts
LOGS_DIR?=/tmp
FAST_MODE?=false
GOPATH?=$(shell go env GOPATH)
WITH_CLEVELDB?=false
INSTALL_TENDERMINT_LOCALLY?=false
TENDERMINT_SRC=$(GOPATH)/src/github.com/tendermint/tendermint
TM_NETWORKS_SRC=$(GOPATH)/src/github.com/interchainio/tm-load-test
TM_OUTAGE_SIM_SERVER_BIN=$(TM_NETWORKS_SRC)/build/tm-outage-sim-server
NETWORK_CONFIG_SCRIPT?=./scripts/001-default.sh
CONFIG_SCRIPT?=$(NETWORK_CONFIG_SCRIPT)
NETWORK_VALIDATORS?=4
VALIDATORS?=$(NETWORK_VALIDATORS)
UNAME_S?=$(shell uname -s)
.PHONY: deploy fetch-tm-logs status

deploy:
	@FAST_MODE=$(FAST_MODE) \
		WITH_CLEVELDB=$(WITH_CLEVELDB) \
		UNAME_S=$(UNAME_S) \
		INSTALL_TENDERMINT_LOCALLY=$(INSTALL_TENDERMINT_LOCALLY) \
		TENDERMINT_SRC=$(TENDERMINT_SRC) \
		TM_NETWORKS_SRC=$(TM_NETWORKS_SRC) \
		./scripts/build-executables.sh
	@rm -rf /tmp/nodes/
	@echo "Generating Tendermint configuration for ${VALIDATORS} validators..."
	@tendermint testnet \
		--v $(VALIDATORS) \
		--n 0 \
		--o /tmp/nodes/ \
		--hostname-prefix tik \
		--node-dir-prefix tik
	@echo "Updating generated Tendermint node configuration..."
	@$(CONFIG_SCRIPT)
	ansible-playbook -i $(INVENTORY) \
		-e "{\"fast_mode\":${FAST_MODE}}" \
		-e "{\"with_cleveldb\":${WITH_CLEVELDB}}" \
		-e "{\"tm_outage_sim_server_bin\":\"${TM_OUTAGE_SIM_SERVER_BIN}\"}" \
		deploy.yml
	@echo "Checking if Tendermint nodes are up..."
	@./scripts/check-status.sh

fetch-tm-logs:
	ansible-playbook -i $(INVENTORY) fetch-tm-logs.yml

status:
	@SLEEP=0 ./scripts/check-status.sh
