GOPATH?=$(shell go env GOPATH)
# NOTE: INVENTORY variable *must* be absolute path
INVENTORY?=inventory/hosts
LOGS_DIR?=/tmp
.PHONY: distribute-pubkey distribute-known_hosts

include common/make/python-ansible.mk

# Deploy test network with the specified name
deploy\:%: venv/bin/ansible
	@$(ACTIVATE_VENV); \
	echo "Deploying network \"$*\"..."; \
	$(MAKE) -C networks/$*/ deploy

# Fetch Tendermint logs from remote machines for the given network
fetch-tm-logs\:%: venv/bin/ansible
	@$(ACTIVATE_VENV); \
	$(MAKE) -C networks/$*/ fetch-tm-logs

# Check the status of the given network
status\:%: venv/bin/ansible
	@$(MAKE) -C networks/$*/ status

# Execute the specified test scenario
scenario\:%: venv/bin/ansible
	@echo "Executing scenario: $*..."
	@$(ACTIVATE_VENV); \
	$(MAKE) -C scenarios/$*/ execute

# Plot results for the specific scenario
plot-results\:%: venv
	@$(ACTIVATE_VENV); \
	$(MAKE) -C scenarios/$*/ plot-results

PUBLIC_KEY_PATH?=/tmp/pubkey.pub
PUBLIC_KEY_STATE?=present
distribute-pubkey:
	@$(ACTIVATE_VENV); \
	ansible-playbook -i $(INVENTORY) \
		-e "{\"public_key_path\": \"${PUBLIC_KEY_PATH}\"}" \
		-e "{\"public_key_state\": \"${PUBLIC_KEY_STATE}\"}" \
		utils/distribute-pubkey.yml

KNOWN_HOSTS_PATH?=/tmp/known_hosts
distribute-known_hosts:
	@$(ACTIVATE_VENV); \
	ansible-playbook -i $(INVENTORY) \
		-e "{\"known_hosts_path\": \"${KNOWN_HOSTS_PATH}\"}" \
		utils/distribute-known_hosts.yml
