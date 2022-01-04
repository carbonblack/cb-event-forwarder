#!/bin/bash

set -e

if [ -z "$1" ]; then
  echo Error: Missing rpm file location parameter.  Ex: ./run_smoketest.sh path/to/rpm
  exit 1
fi

RPM_FILE=$(find "$1" -name "*.rpm" -print -quit)

SYSTEM_CTL_PATCH="https://${ARTIFACTORY_SERVER}/artifactory/cb/gdraheim/docker-systemctl-replacement/1.4.3424/systemctl.py"
if [[ "$(cat /etc/redhat-release)" == *"release 8"* ]]; then
  SYSTEM_CTL_PATCH="https://${ARTIFACTORY_SERVER}/artifactory/cb/gdraheim/docker-systemctl-replacement/1.4.3424/systemctl3.py"
fi

echo Adding cb user
groupadd cb --gid 8300 && \
useradd --shell /sbin/nologin --gid cb --comment "Service account for VMware Carbon Black EDR" -M cb

echo Running smoke test on file: "$RPM_FILE"

rpm -ivh "$RPM_FILE"

mkdir -p /etc/sudoers.d
touch /etc/sudoers.d/cb-ef

cp $2/cb-event-forwarder.all.ini /etc/cb/integrations/event-forwarder/cb-event-forwarder.conf
export EF_CANNED_INPUT=$3
echo Starting service...
service cb-event-forwarder start

sleep 5
tail -n 1 /tmp/event_bridge_output.json
filepath="/tmp/event_bridge_output.json"
if [ -n "$(find "$filepath" -prune -size +1000000c)" ]; then
    echo "event forwarder working ok!"
else
    echo "Event Forwarder not working correctly - exiting"
    exit 1
fi
systemctl stop cb-event-forwarder

echo "HTTP OUTPUT TEST"
sed -i 's/output_type=file/output_type=http/g' /etc/cb/integrations/event-forwarder/cb-event-forwarder.conf
touch /tmp/httpoutput
python3.8 $2/test_server.py 127.0.0.1 8080 /tmp/httpoutput http &
sleep 5
service cb-event-forwarder start
sleep 5
service cb-event-forwarder stop
filepath="/tmp/httpoutput"
if [ -n "$(find "$filepath" -prune -size +1000000c)" ]; then
    echo "event forwarder http output working ok!"
else
    echo "Event Forwarder http output not working correctly - exiting"
    exit 1
fi
systemctl stop cb-event-forwarder
kill -9 `jobs -p`
sleep 5

echo "TCP OUTPUT TEST"
sed -i 's/output_type=http/output_type=tcp/g' /etc/cb/integrations/event-forwarder/cb-event-forwarder.conf
touch /tmp/tcpoutput
python3.8 $2/test_server.py 127.0.0.1 31337 /tmp/tcpoutput tcp &
sleep 5
service cb-event-forwarder start
sleep 5
service cb-event-forwarder stop
filepath="/tmp/tcpoutput"
if [ -n "$(find "$filepath" -prune -size +1000000c)" ]; then
    echo "event forwarder tcp output working ok!"
else
    echo "Event Forwarder tcp output not working correctly - exiting"
    exit 1
fi
service cb-event-forwarder stop
kill -9 `jobs -p`

echo "SYSLOG OUTPUT TEST"
sed -i 's/output_type=tcp/output_type=syslog/g' /etc/cb/integrations/event-forwarder/cb-event-forwarder.conf
touch /tmp/syslogoutput
python3.8 $2/test_server.py 127.0.0.1 31337 /tmp/syslogoutput tcp &
sleep 5
service cb-event-forwarder start
sleep 5
service cb-event-forwarder stop
filepath="/tmp/syslogoutput"
if [ -n "$(find "$filepath" -prune -size +1000000c)" ]; then
    echo "event forwarder syslog output working ok!"
else
    echo "Event Forwarder syslog output not working correctly - exiting"
    exit 1
fi
service cb-event-forwarder stop
kill -9 `jobs -p`
sleep 5

echo "SPLUNK OUTPUT TEST"
sed -i 's/output_type=syslog/output_type=splunk/g' /etc/cb/integrations/event-forwarder/cb-event-forwarder.conf
touch /tmp/splunkoutput
python3.8 $2/test_server.py 127.0.0.1 8080 /tmp/splunkoutput http &
sleep 5
service cb-event-forwarder start
sleep 5
service cb-event-forwarder stop
filepath="/tmp/splunkoutput"
if [ -n "$(find "$filepath" -prune -size +1000000c)" ]; then
    echo "event forwarder splunk output working ok!"
else
    echo "Event Forwarder splunk output not working correctly - exiting"
    exit 1
fi
systemctl stop cb-event-forwarder
kill -9 `jobs -p`
sleep 5

yum -y remove cb-event-forwarder
