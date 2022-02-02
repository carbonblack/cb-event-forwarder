#!/bin/bash
LABEL=edreventforwarder
IMAGE=eventforwarder/centos7:latest
CONFIG_DIR_EXTERNAL=/etc/cb/integrations/event-forwarder
CONFIG_DIR=/etc/cb/integrations/event-forwarder
LOG_DIR_EXTERNAL=/var/log/cb/integrations/cb-event-connector
LOG_DIR=/var/log/cb/integrations/cb-event-connector
MOUNT_POINTS="--mount type=bind,source=$CONFIG_DIR_EXTERNAL,target=$CONFIG_DIR --mount type=bind,source=$LOG_DIR_EXTERNAL,target=$LOG_DIR"
SERVICE_START='systemctl start cb-event-connector'
CONTAINER_RUNNING=$(docker inspect --format="{{.State.Running}}" $LABEL 2> /dev/null)
if [ "$CONTAINER_RUNNING" == "true" ]; then
  STARTUP_COMMAND="echo EDR Event Forwarder container already running"
  STATUS_COMMAND="docker exec $LABEL systemctl status cb-event-connector"
else
  STARTUP_COMMAND="docker run -d --rm $MOUNT_POINTS --name $LABEL $IMAGE $SERVICE_START"
  STATUS_COMMAND="echo EDR Event Forwarder container is stopped"
fi
SHUTDOWN_COMMAND="docker stop $LABEL"


print_help() {
  echo "Usage: edr-eventforwarder-run COMMAND [options]"
  echo
  echo "Options:"
  echo "  -h, --help             Print this help message."
  echo
  echo "COMMANDs:"
  echo "  start        Start the connector"
  echo "  stop       Stop the connector"
  echo "  status         Stop the connector"
  exit 2
}

PARSED=$(getopt -n run -o o: --long osversion:,help -- "$@")

if [ "${?}" != "0" ]; then
  print_help
fi

if [[ "${1}" == "" ]]; then
  echo "COMMAND required"; print_help
fi

if [[ "${1^^}" =~ ^(START|STOP|STATUS)$ ]]; then
  echo "EDR Event Forwarder: running ${1}..."
  case "${1^^}" in
    START) $STARTUP_COMMAND ;;
    STOP) $SHUTDOWN_COMMAND ;;
    STATUS) $STATUS_COMMAND ;;
  esac
else
  echo "run: invalid command '${1}'"; print_help
fi
