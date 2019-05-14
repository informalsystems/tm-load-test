# tm-outage-sim-server

A simple [Tendermint](https://tendermint.com) outage simulator server to allow
one to easily start or stop Tendermint nodes for experimentation and QA
purposes.

## Overview
`tm-outage-sim-server` runs a simple HTTP server alongside a Tendermint node. It
allows one to send an `up` or `down` command via an HTTP POST request to bring
the Tendermint service up or down, respectively.

## Security
**Do not run this service continuously alongside a production Tendermint
instance**. Despite the fact that the current version of `tm-outage-sim-server`
includes a basic HTTP authentication requirement, this might still allow an
attacker to stop your Tendermint node should they discover your password.

This application requires the relevant system privileges to be able to interact
with the Tendermint service. For example, if a user `outage-sim` were
responsible for running this server, one could put the following configuration
in `/etc/sudo.d/outage-sim`:

```
outage-sim ALL=(ALL) NOPASSWD: /bin/systemctl start tendermint, /bin/systemctl stop tendermint, /sbin/pidof tendermint
```

This is because `tm-outage-sim-server` calls all of the above commands to
interact with the Tendermint service via `sudo` calls.

See [this discussion](https://unix.stackexchange.com/q/215412) for more details.

## Requirements
The following are minimum requirements for running this application:

* A UNIX variant that uses systemd
* Correctly configured `sudo` privileges for the user that will be running the
  `tm-outage-sim-server` service.
* Tendermint v0.29.1+

To build `tm-outage-sim-server`, you will need:

* Golang v1.12+

## Running
To run the application alongside Tendermint as a `systemd` service, use the
following `systemd` configuration file
(`/etc/systemd/system/tm-outage-sim-server.service`):

```
[Unit]
Description=Tendermint Outage Simulator
Requires=network-online.target
After=network-online.target

[Service]
Restart=on-failure
User=outage-sim
Group=outage-sim
PermissionsStartOnly=true
ExecStart=/usr/bin/tm-outage-sim-server -addr 0.0.0.0:26680 -u loadtest -p "#2a#12#ac6f8zq9vvugNb3QXeOV9.RGFHeu8a7qhf9WIRAfH69a0k2j7J7wy"
StandardOutput=syslog
StandardError=syslog
SyslogIdentifier=outage-sim

[Install]
WantedBy=multi-user.target
```

Note how the bcrypt hash above has its `$` symbols replaced with `#`. One could
simply escape the `$` symbols too, however, but `tm-outage-sim-server` supports
bcrypt hashes specified in this alternative format to make such files marginally
more readable.

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
