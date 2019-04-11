[![CircleCI](https://circleci.com/gh/interchainio/tm-load-test/tree/master.svg?style=svg)](https://circleci.com/gh/interchainio/tm-load-test/tree/master)

# tm-load-test

`tm-load-test` is a distributed load testing tool (and framework) for load
testing [Tendermint](https://tendermint.com/) networks.

## Requirements
In order to build and use the tools, you will need:

* Go 1.11.5+
* `make`

## Building
To build the `tm-load-test` and `tm-outage-sim-server` binaries into the `build`
directory:

```bash
> make
```

## Development
To run the linter and the tests:

```bash
> make lint
> make test
```

## Scripts
`tm-load-test` is tedious to use across many machines without some form of
automation, so to help along those lines there are
[Ansible](https://docs.ansible.com/ansible/latest/index.html) scripts in the
[`scripts/load-testing`](./scripts/load-testing/README.md) folder.
