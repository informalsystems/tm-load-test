#!/bin/bash
set -e

NODE_DOMAIN=${NODE_DOMAIN:-"sredev.co"}

for CFG_FILE in $(find /tmp/nodes -name 'config.toml'); do
    NODE_ID=$(basename $(dirname $(dirname ${CFG_FILE})))
    sed -i.orig \
        -e "s/t\([ioa]\)k\([0-9]*\):/t\1k\2.${NODE_DOMAIN}:/g" \
        -e "s/^proxy_app = \(.*\)$/proxy_app = \"kvstore\"/" \
        -e "s/^moniker = \(.*\)$/moniker = \"${NODE_ID}\"/" \
        -e "s/^db_backend = \(.*\)$/db_backend = \"cleveldb\"/" \
        -e "s/^log_format = \(.*\)$/log_format = \"json\"/" \
        -e "s/^create_empty_blocks = \(.*\)$/create_empty_blocks = false/" \
        -e "s/^prometheus = \(.*\)$/prometheus = true/" \
        -e "s/^addr_book_strict = false$/addr_book_strict = true/" \
        -e "s/^recheck = true$/recheck = false/" \
        -e "s/^size = 5000$/size = 50000/" \
        ${CFG_FILE}
    echo "Rewrote \"config.toml\" for ${NODE_ID}"
done
