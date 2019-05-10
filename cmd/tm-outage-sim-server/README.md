# tm-outage-sim-server

A simple [Tendermint](https://tendermint.com) outage simulator server for use on
CentOS and Debian/Ubuntu-derived operating systems.

## Overview
`tm-outage-sim-server` runs a simple HTTP server alongside a Tendermint node. It
allows one to send an `up` or `down` command via an HTTP POST request to bring
the Tendermint service up or down, respectively.

## Security
**Do not run this service continuously alongside a production Tendermint
instance**. Despite the fact that the current version of `tm-outage-sim-server`
includes a basic HTTP authentication requirement, this might still allow an
attacker to stop your Tendermint node.

This application requires the relevant system privileges to be able to interact
with the Tendermint service. See [this
discussion](https://unix.stackexchange.com/q/215412) for an example as to how to
configure a non-root user to have the relevant sudo privileges to start/stop a
service.

## Requirements
The following are minimum requirements for running this application:

* CentOS, Debian or Ubuntu (or Debian-derived OS)
* Root privileges for the service (as it needs to control the Tendermint service
  on the same machine)
* Tendermint v0.29.1+

To build `tm-outage-sim-server`, you will need:

* Golang v1.12+

## Running
To run the application alongside Tendermint as a `systemd` service, use the
following `systemd` configuration file
(`/etc/systemd/system/tm-outage-sim-server.service`):

```
[Unit]
Description=Tendermint Outage Simulator Server
Requires=network-online.target
After=network-online.target

[Service]
Restart=on-failure
User=tm-outage-sim
Group=tm-outage-sim
PermissionsStartOnly=true
ExecStart=/usr/bin/tm-outage-sim-server -p "\$2a\$12$ac6f8zq9vvugNb3QXeOV9.RGFHeu8a7qhf9WIRAfH69a0k2j7J7wy"
StandardOutput=syslog
StandardError=syslog
SyslogIdentifier=tm-outage-sim-server

[Install]
WantedBy=multi-user.target
```

Then:

```bash
# Make sure systemd finds the new service
systemctl daemon-reload

# Start the outage simulator
service tm-outage-sim-server start
```

By default, the server binds to `0.0.0.0:26680`.

## Usage
To bring the Tendermint service up or down, simply do an HTTP POST to the
server:

```bash
# Bring Tendermint up
curl -s -X POST -d "up" http://tm-load-test:testpassword@127.0.0.1:26680

# Bring Tendermint down
curl -s -X POST -d "down" http://tm-load-test:testpassword@127.0.0.1:26680
```
