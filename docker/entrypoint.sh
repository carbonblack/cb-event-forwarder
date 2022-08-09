#!/usr/bin/env bash
set -e

if [[ ! -d /etc/cb/integrations/event-forwarder ]]; then
    mkdir -p /etc/cb/integrations/event-forwarder
fi

if [[ ! -f /etc/cb/integrations/event-forwarder/cb-event-forwarder.conf ]]; then
    cp  /root/cb-event-forwarder.conf /etc/cb/integrations/event-forwarder/cb-event-forwarder.conf
fi

if [[ ! -f /root/event-forwarder-edr/cb-event-forwarder.conf ]]; then
    cp  /etc/cb/integrations/event-forwarder/cb-event-forwarder.conf /root/event-forwarder-edr/cb-event-forwarder.conf
fi

# EDR container name should always be 'carbonblack-edr' ergo making it the name we connect to
sed -i 's/^cb_server_hostname.*/cb_server_hostname = carbonblack-edr/' /etc/cb/integrations/event-forwarder/cb-event-forwarder.conf

# Bug fix for 7.7.0 which doesn't create the data/config/integrations/event-forwarder dir but requires it to be there.
# Mounting it via the compose.yml file will cause it to be created, but with root:root permissions. This fixes those
# permissions to allow the cb:cb user (8300:8300 inside the edr container) to access and change those files
chown 8300:8300 /root/event-forwarder-edr
chown 8300:8300 /root/event-forwarder-edr/*

# Run the command
/usr/share/cb/integrations/event-forwarder/go-serviced