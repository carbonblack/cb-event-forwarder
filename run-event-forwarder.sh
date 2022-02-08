#!/bin/bash
LABEL=edreventforwarder
IMAGE=eventforwarder/centos7:latest
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
        STOPPED_NAME=$(docker ps | grep $LABEL | head -n1 | awk '{print $1}')
        if [ "${#STOPPED_NAME}" -gt 0 ]; then
            echo "EDR Event Forwarder Container status: Stopped "
        else
            echo "EDR Event Forwarder Container status: No running container"
        fi
        CONTAINER_RUNNING=false
    fi
}

STATUS_COMMAND=get_container_status

SHUTDOWN_COMMAND=stop_and_remove_container
stop_and_remove_container() {
    docker stop $LABEL > /dev/null
    docker rm $LABEL > /dev/null
}

STARTUP_COMMAND="docker run -d --rm $MOUNT_POINTS --name $LABEL $IMAGE $SERVICE_START"

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
  echo "EDR Event Forwarder: running command ${1}..."
  case "${1^^}" in
    START) $STARTUP_COMMAND ;;
    STOP) $SHUTDOWN_COMMAND ;;
    STATUS) $STATUS_COMMAND ;;
  esac
else
  echo "run: invalid command '${1}'"; print_help
fi
