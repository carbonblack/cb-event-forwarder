#!/bin/bash

set -e

if [ -z "$1" ]; then
  echo Error: Missing rpm file location parameter.  Ex: ./run_smoketest.sh path/to/rpm
  exit 1
fi

echo Adding cb user
groupadd cb --gid 8300 && \
useradd --shell /sbin/nologin --gid cb --comment "Service account for VMware Carbon Black EDR" -M cb

mkdir -p /etc/sudoers.d
touch /etc/sudoers.d/cb-ef

cp $2 /etc/cb/integrations/event-forwarder/cb-event-forwarder.conf
export EF_CANNED_INPUT=$3
echo Starting service...
timeout 5 /usr/share/cb/integrations/event-forwarder/cb-event-forwarder /etc/cb/integrations/event-forwarder/cb-event-forwarder.conf || true
echo Done running

tail -n 1 /tmp/event_bridge_output.json
filepath="/tmp/event_bridge_output.json"
if [ -n "$(find "$filepath" -prune -size +1000000c)" ]; then
    echo "event forwarder working ok!"
else
    echo "Event Forwarder not working correctly - exiting"
    exit 1
fi