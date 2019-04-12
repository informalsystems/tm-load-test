#!/bin/sh
set -e

for FILENAME in $(find /tmp/ -name 'ifconfig-*' | sort); do
    HOST=`echo ${FILENAME} | sed -e "s/.*ifconfig-\(.*\)/\1/"`
    IP_ADDR=`cat ${FILENAME} | grep 'inet ' | sed -e "s/.*inet \([0-9.]*\).*/\1/"`
    echo "${IP_ADDR}\t${HOST}"
done
