#!/bin/bash

print_help() {
  echo "Usage: run-event-forwarder.sh [options] COMMAND"
  echo
  echo "Options:"
  echo "  -o, --osversion [7|8|9]  The CentOS/RHEL version of the container image to use. Default is 7."
  echo "  -h, --help             Print this help message."
  echo
  echo "COMMANDs:"
  echo "  start        Start the connector"
  echo "  stop         Stop the connector"
  echo "  status       Show connector status"
  exit 2
}

OSVERSION=7
PARSED=$(getopt -n run-event-forwarder -o o:h --long osversion:,help -- "$@")
eval set -- "$PARSED"
while :; do
  case "$1" in
    -o | --osversion) OSVERSION="$2"; shift 2 ;;
    -h | --help) print_help ;;
    --) shift; break ;;
    *) break ;;
  esac
done

LABEL=edreventforwarder
if [ "${OSVERSION}" == "9" ]; then
  IMAGE=eventforwarder/rocky${OSVERSION}:latest
else
  IMAGE=eventforwarder/centos${OSVERSION}:latest
fi
CONFIG_DIR_EXTERNAL=/etc/cb/integrations/event-forwarder
CONFIG_DIR=/etc/cb/integrations/event-forwarder
LOG_DIR_EXTERNAL=/var/log/cb/integrations/cb-event-forwarder
LOG_DIR=/var/log/cb/integrations/cb-event-forwarder
MOUNT_POINTS="--mount type=bind,source=$CONFIG_DIR_EXTERNAL,target=$CONFIG_DIR --mount type=bind,source=$LOG_DIR_EXTERNAL,target=$LOG_DIR"
SERVICE_START=/usr/share/cb/integrations/event-forwarder/cb-event-forwarder

get_container_status () {
    CONTAINER_NAME=$(docker ps | grep $LABEL | head -n1 | awk '{print $1}')
    if [ "${#CONTAINER_NAME}" -gt 0 ]; then
        CONTAINER_RUNNING=true
        echo "EDR Event Forwarder Container status: Running"
        echo "EDR Event Forwarder Container identifier: ${CONTAINER_NAME}"
    else
        # run ps with -a switch to see if stopped or non-existent
        STOPPED_NAME=$(docker ps -a | grep $LABEL | head -n1 | awk '{print $1}')
        if [ "${#STOPPED_NAME}" -gt 0 ]; then
            echo "EDR Event Forwarder Container status: Stopped"
        else
            echo "EDR Event Forwarder Container status: No running container"
        fi
        CONTAINER_RUNNING=false
    fi
}

STATUS_COMMAND=get_container_status

stop_and_remove_container() {
    docker stop $LABEL > /dev/null
    docker rm $LABEL > /dev/null
}
SHUTDOWN_COMMAND=stop_and_remove_container

STARTUP_COMMAND="docker run -d --restart unless-stopped $MOUNT_POINTS --name $LABEL $IMAGE $SERVICE_START"

if [[ "${1}" == "" ]]; then
  echo "COMMAND required"; print_help
fi

if [[ "${1^^}" =~ ^(START|STOP|STATUS)$ ]]; then
  echo "EDR Event Forwarder: running command ${1}..."
  case "${1^^}" in
    START) $STARTUP_COMMAND ;;
    STOP) $SHUTDOWN_COMMAND ;;
    STATUS) $STATUS_COMMAND ;;
  esac
else
  echo "run: invalid command '${1}'"; print_help
fi
