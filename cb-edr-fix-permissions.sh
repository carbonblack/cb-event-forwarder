#!/usr/bin/env bash
#
# This script sets the necessary permissions so that CB EDR can configure and run
# cb-event-forwarder from the EDR user console. It requires that both EDR and
# cb-event-forwarder are installed.
#

if [[ $EUID -ne 0 ]]; then
  echo "This script must be run by a root user"
  exit 1
fi

id -u cb >/dev/null 2>&1
if [[ $? -ne 0 ]]; then
  echo "No cb user, please install CB EDR before running this script"
  exit 2
fi

chown cb:cb /etc/cb/integrations/event-forwarder/cb-event-forwarder.conf
chown -R cb:cb /usr/share/cb/integrations/event-forwarder
chown -R cb:cb /etc/cb/integrations/event-forwarder

if [[ ! -d /var/cb/data/event-forwarder ]]; then
  mkdir -p /var/cb/data/event-forwarder
fi
chown -R cb:cb /var/cb/data/event-forwarder

if [[ ! -d /var/log/cb/integrations/cb-event-forwarder ]]; then
  mkdir -p /var/log/cb/integrations/cb-event-forwarder
fi
chown -R cb:cb /var/log/cb/integrations/cb-event-forwarder

(
  cat <<'__EOF__'
## Required for Event Forwarder control
cb ALL=(ALL)  NOPASSWD: /usr/bin/systemctl start cb-event-forwarder
cb ALL=(ALL)  NOPASSWD: /usr/bin/systemctl stop cb-event-forwarder
cb ALL=(ALL)  NOPASSWD: /usr/bin/systemctl restart cb-event-forwarder
cb ALL=(ALL)  NOPASSWD: /usr/bin/systemctl status cb-event-forwarder
cb ALL=(ALL)  NOPASSWD: /usr/bin/systemctl enable --now cb-event-forwarder
cb ALL=(ALL)  NOPASSWD: /usr/bin/systemctl disable --now cb-event-forwarder
cb ALL=(ALL)  NOPASSWD: /usr/bin/chkconfig --add cb-event-forwarder
cb ALL=(ALL)  NOPASSWD: /usr/bin/chkconfig --del cb-event-forwarder
cb ALL=(ALL)  NOPASSWD: /usr/bin/chkconfig cb-event-forwarder off
__EOF__
) >/etc/sudoers.d/cb-ef
