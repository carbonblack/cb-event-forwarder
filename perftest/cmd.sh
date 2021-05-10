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

echo "starting minio s3 compatible storage"
mkdir /tmp/s3bucket
rm -f /tmp/s3bucket/*
./minio server /tmp &>/dev/null &
echo Running perf test on file: "$RPM_FILE"

rpm -ivh "$RPM_FILE"

mkdir -p /etc/sudoers.d
touch /etc/sudoers.d/cb-ef

mkdir -p ~/.aws ; touch ~/.aws/credentials ; printf "[default]\naws_access_key_id=minioadmin\naws_secret_access_key=minioadmin" > ~/.aws/credentials
export EF_CANNED_INPUT=$3

echo Starting Perf Tests for S3

cp "$2"/cb-event-forwarder.gzip.ini /etc/cb/integrations/event-forwarder/cb-event-forwarder.conf
systemctl start cb-event-forwarder
sleep 10
echo "~~~ GZIP RESULTS ~~~"
curl -s http://localhost:33706/debug/metrics | grep core.output.data.mean
systemctl stop cb-event-forwarder

rm /tmp/s3bucket/*
cp "$2"/cb-event-forwarder.lz4.ini /etc/cb/integrations/event-forwarder/cb-event-forwarder.conf
systemctl start cb-event-forwarder
sleep 10
echo "~~~ LZ4 RESULTS ~~~"
curl -s http://localhost:33706/debug/metrics | grep core.output.data.mean
systemctl stop cb-event-forwarder

echo PERF TESTS COMPLETE
