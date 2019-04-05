# tm-outage-sim-server

A simple [Tendermint](https://tendermint.com) outage simulator server for use on
CentOS and Debian/Ubuntu-derived operating systems.

## Overview
`tm-outage-sim-server` runs a simple HTTP server alongside a Tendermint node. It
allows one to send an `up` or `down` command via an HTTP POST request to bring
the Tendermint service up or down, respectively.

## Security
**Do not run this service continuously alongside a production Tendermint
instance**. This service is exclusively designed to help with configuring
short-term, repeatable experiments. Running this service in a production
environment where it is accessible from the public Internet could allow someone
to bring down your Tendermint instance.

## Requirements
The following are minimum requirements for running this application:

* CentOS, Debian or Ubuntu (or Debian-derived OS)
* Root privileges for the service (as it needs to control the Tendermint service
  on the same machine)
* Tendermint v0.29.1+

To build `tm-outage-sim-server`, you will need:

* Golang v1.11.5+

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
User=root
Group=root
PermissionsStartOnly=true
ExecStart=/usr/bin/tm-outage-sim-server
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

By default, the server binds to `0.0.0.0:34000`.

## Usage
To bring the Tendermint service up or down, simply do an HTTP POST to the
server:

```bash
# Bring Tendermint up
curl -s -X POST -d 'up' http://127.0.0.1:34000

# Bring Tendermint down
curl -s -X POST -d 'down' http://127.0.0.1:34000
```
