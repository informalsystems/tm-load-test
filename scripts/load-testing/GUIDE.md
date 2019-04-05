# Tendermint Networks Load Testing Guide

The combination of the [`tm-load-test`](../../cmd/tm-load-test/README.md)
application and the scripts in this folder should assist in performing your own
load testing of your Tendermint network.

The aim of this guide will be to help you set up:

* a 4-node Tendermint network using a relatively standard configuration running
  the `kvstore` ABCI application
* 1 load testing master node
* 3 load testing slave nodes

## Step 1: Set up your local machine
The scripts in this folder will, depending on your instructions/configuration,
need to build Tendermint and the `tm-load-test` tool. The scripts also make
extensive use of [Ansible](https://docs.ansible.com/ansible/latest/user_guide/)
for managing software on your VMs (including installing Tendermint).

Therefore, the following software must be installed on your local machine:

* Go 1.11+
* Python 3.6+ (on some Linux distros, you will also need the `python3-venv`
  package to be able to create/manage virtual environments)
* A build toolchain (on CentOS, run `yum groupinstall 'Development Tools`, and
  on Ubuntu run `apt install build-essential` - both as root)

Make sure your `GOPATH` environment variable is also set up correctly.

Then, you'll need both the Tendermint source code, and the Tendermint Networks
code on your local machine. The most convenient place to put them is in your
`GOPATH`:

```bash
go get -u github.com/tendermint/tendermint/...
go get -u github.com/interchainio/tm-load-test/...
```

## Step 2: Set up your VMs
In total, you will need 8 VMs to follow the steps in this guide. Right now, the
scripts only support CentOS-based machines. Make sure these machines have Python
installed.

We assume that:

* Your Tendermint nodes' IP addresses are accessible via `192.168.1.{2..5}`
* Your load testing nodes' IP addresses are accessible via `192.168.1.{6..9}`
* A non-root user called `loadtester` has `sudo` access on all of the nodes

Substitute with your own IP addresses/credentials above.

## Step 3: Configure your local inventory
We need our scripts to know where the target machines are, and how to access
them.

Take a look at the example [`inventory/hosts`](./inventory/hosts) file, and
create another file in the same directory called `myhosts`:

```ini
[tendermint]
tm1 ansible_host=192.168.1.2 ansible_user=loadtester
tm2 ansible_host=192.168.1.3 ansible_user=loadtester
tm3 ansible_host=192.168.1.4 ansible_user=loadtester
tm4 ansible_host=192.168.1.5 ansible_user=loadtester

[loadtest]
load1 ansible_host=192.168.1.6 ansible_user=loadtester
load2 ansible_host=192.168.1.7 ansible_user=loadtester
load3 ansible_host=192.168.1.8 ansible_user=loadtester
load4 ansible_host=192.168.1.9 ansible_user=loadtester
```

## Step 4: Set up SSH access to all of the machines
Since Ansible uses SSH to interact with your VMs, you'll need to tell your local
SSH client to trust the remote machines.

```bash
ssh-keyscan -H 192.168.1.2 >> ~/.ssh/known_hosts
ssh-keyscan -H 192.168.1.3 >> ~/.ssh/known_hosts
ssh-keyscan -H 192.168.1.4 >> ~/.ssh/known_hosts
ssh-keyscan -H 192.168.1.5 >> ~/.ssh/known_hosts
# ... etc. for all VMs
```

## Step 5: Set up a basic Tendermint test network
You should now be able to spin up your 4-node Tendermint network using the
[reference deployment configuration](./networks/001-reference/README.md):

```bash
cd ${GOPATH}/src/github.com/interchainio/tm-load-test/scripts/load-testing/

# Set up a shortcut for specifying the load testing scripts path
export LTSPATH=${GOPATH}/src/github.com/interchainio/tm-load-test/scripts/load-testing

# Override the INVENTORY parameter for the deployment script
# NOTE: This must be an *absolute* path
INVENTORY=${LTSPATH}/inventory/myhosts \
    make deploy:001-reference
```

This will set up a Python virtual environment in your local
`scripts/load-testing` folder and install Ansible (a once-off operation). The
first run may take a while.

## Step 6: Execute a simple single load test
The
[`003-kvstore-loadtest-distributed`](./scenarios/003-kvstore-loadtest-distributed/README.md)
load testing scenario executes a single load test against a Tendermint network
of our choosing (assuming that Tendermint network's running the `kvstore` ABCI
application). To run it:

```bash
# This is a JavaScript array of configuration parameters for the "tendermint"
# test network specified above.
# In future, specifying the test network targets will definitely be made
# easier, but for now it's a bit of a manual process.
export TEST_NETWORK_TARGETS=`echo '[
    { id: "tm1", url: "http://tm1:26657", prometheus_urls: "tendermint=http://tm1:26660,node=http://tm1:9100/metrics" },
    { id: "tm2", url: "http://tm2:26657", prometheus_urls: "tendermint=http://tm2:26660,node=http://tm2:9100/metrics" },
    { id: "tm3", url: "http://tm3:26657", prometheus_urls: "tendermint=http://tm3:26660,node=http://tm3:9100/metrics" },
    { id: "tm4", url: "http://tm4:26657", prometheus_urls: "tendermint=http://tm4:26660,node=http://tm4:9100/metrics" }
]' | tr '\n' ' '`

# Execute the scenario, but give it some configuration parameters:
# - We want to use our "myhosts" inventory, specified above
# - Output all results to a folder called /tmp/myloadtestresults/ (will
#   automatically be created)
# - Make the host with ID "load1" the master node for load testing (all others
#   will automatically be slaves)
# - Tell the master that we only expect 3 slaves
# - Spawn 100 clients per slave node
# - Spawn clients at a rate of 10 per second
# - Let each client only execute 10 interactions before being considered
#   complete
#
# The script will pick up the Tendermint nodes' addresses from the
# TEST_NETWORK_TARGETS environment variable we just set.
INVENTORY=${LTSPATH}/inventory/myhosts \
    LOCAL_RESULTS_DIR=/tmp/myloadtestresults \
    MASTER_NODE=load1 \
    EXPECT_SLAVES=3 \
    CLIENTS_SPAWN=100 \
    CLIENTS_SPAWN_RATE=10 \
    CLIENTS_MAX_INTERACTIONS=10 \
    make scenario:003-kvstore-loadtest-distributed
```

While this load test is running, all you will see is the Ansible output line:

```
TASK [Execute the load test] **************************************************
```

To see progress, simply SSH into your master node, `sudo su` and tail the
`stdout.log` file:

```bash
ssh loadtester@load1
sudo su
cd /home/loadtest
tail -f stdout.log

...

time="2019-04-02T01:09:57Z" level=info msg="Accepted enough connected slaves - starting load test" ctx=master slaveCount=4
time="2019-04-02T01:09:57Z" level=debug msg="Broadcasting message to all slaves" ctx=master fields.msg="&StartLoadTest{Sender:172.31.6.229:35000/master,}"
time="2019-04-02T01:09:57Z" level=debug msg="Broadcasting message to slave" ctx=master pid="{172.31.9.202:35000 e36468ff-20aa-4976-a2da-8c95c5593e20 <nil>}"
time="2019-04-02T01:09:57Z" level=debug msg="Broadcasting message to slave" ctx=master pid="{172.31.1.188:35000 94f5d4a3-e858-49ae-877e-57381b7ddb76 <nil>}"
time="2019-04-02T01:09:57Z" level=debug msg="Broadcasting message to slave" ctx=master pid="{172.31.8.10:35000 4d54d3b9-f349-4133-b71a-146d6f902973 <nil>}"
2019/04/02 01:09:57 [REMOTE] time="2019-04-02T01:09:57Z" level=debug msg="Broadcasting message to slave" ctx=master pid="{172.31.12.66:35000 0f77554b-beab-4fad-be6f-b3aaf6f2612b <nil>}"
Started EndpointWriter address="172.31.12.66:35000" 
2019/04/02 01:09:57 [REMOTE] EndpointWriter connecting address="172.31.12.66:35000" 
2019/04/02 01:09:57 [REMOTE] EndpointWriter connected address="172.31.8.10:35000" 
2019/04/02 01:09:57 [REMOTE] Started EndpointWatcher address="172.31.12.66:35000" 
2019/04/02 01:09:57 [REMOTE] EndpointWriter connected address="172.31.12.66:35000" 
time="2019-04-02T01:10:17Z" level=info msg="Progress update" approxTimeLeft=01h12m26s completed="0.46%" ctx=master interactionsPerSec=91.62
time="2019-04-02T01:10:37Z" level=info msg="Progress update" approxTimeLeft=00h40m22s completed="1.63%" ctx=master interactionsPerSec=162.44
time="2019-04-02T01:10:55Z" level=debug msg="All slaves connected within timeout limit - no need to terminate master" ctx=master
time="2019-04-02T01:10:57Z" level=info msg="Progress update" approxTimeLeft=00h33m57s completed="2.86%" ctx=master interactionsPerSec=190.73
time="2019-04-02T01:11:17Z" level=info msg="Progress update" approxTimeLeft=00h31m37s completed="4.05%" ctx=master interactionsPerSec=202.32
time="2019-04-02T01:11:37Z" level=info msg="Progress update" approxTimeLeft=00h30m23s completed="5.20%" ctx=master interactionsPerSec=208.05
time="2019-04-02T01:11:57Z" level=info msg="Progress update" approxTimeLeft=00h29m25s completed="6.37%" ctx=master interactionsPerSec=212.24
time="2019-04-02T01:12:17Z" level=info msg="Progress update" approxTimeLeft=00h28m35s completed="7.55%" ctx=master interactionsPerSec=215.59
time="2019-04-02T01:12:37Z" level=info msg="Progress update" approxTimeLeft=00h27m43s completed="8.78%" ctx=master interactionsPerSec=219.45
```

# Step 7: Execute multiple load tests automatically
The
[`004-kvstore-loadtest-distcollection`](./scenarios/004-kvstore-loadtest-distcollection/README.md)
load testing scenario allows for automation of the entire process of:

1. Redeploying a clean Tendermint network (optional)
2. Executing a load test with an incrementing set of parameters (e.g. one can
   increase the number of clients over time)
