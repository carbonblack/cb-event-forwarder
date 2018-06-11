# Cb Response Event Forwarder

## NEW FOR 4.0

* Yaml Configuration
    * The configuration format has been changed from .ini to .yaml. use `key: v` rather than `key=value` format.
* Multilple Forwarders 
    * Run multiple event forwarders 
* Modular Output Plugins
    * Users can write their own modular outputs for the event forwarder, without commiting source upstream.
    * The existing kafka-output has been removed and replaced with a kafka-output plugin using go-kafka-confluent
* Template Encoders For Formatting Output 
    * Users can configure Go-template driven formatting for their Cb Event messages 
* Event Filtering
    * Users can configure Go-Template driven filtering for their Cb Event messages

## Overview

The Cb Response Event Forwarder is a standalone service that will listen on the Cb Response enterprise bus and export
events (both watchlist/feed hits as well as raw endpoint events, if configured) in a normalized JSON or LEEF format.
The events can be saved to a file, delivered to a network service or archived automatically to an Amazon AWS S3 bucket.
These events can be consumed by any external system that accepts JSON or LEEF, including Splunk and IBM QRadar.

The list of events to collect is configurable.
By default all feed and watchlist hits, alerts, binary notifications, and raw sensor events are exported into JSON.  The
configuration file for the connector is stored in `/etc/cb/integrations/event-forwarder/cb-event-forwarder.conf`.

## Support

The pre-built RPM is supported via our [User eXchange (Jive)](https://community.carbonblack.com/community/developer-relations) 
and via email to dev-support@carbonblack.com.  

## Raw Sensor Events 

We have seen a performance impact when exporting all raw sensor events onto the enterprise bus.  We do not recommend
exporting all the events.  The performance impacts are seen when the events are broadcast on the bus, by enabling the
"DatastoreBroadcastEventTypes".  We recommend that at most, only process and netconn events be broadcast on the event
bus. 

## Quickstart Guide

The cb-event-forwarder can be installed on any 64-bit Linux machine running CentOS 6.x. 
It can be installed on the same machine as the Cb Response server, or another machine. 
If you are forwarding a large volume of events to QRadar (for example, all file modifications and/or registry 
modifications), or are forwarding events from a Cb Response cluster, then installing it on a separate machine is recommended. 
Otherwise, it is acceptable to install the cb-event-forwarder on the Cb Response server itself.

### Installation

To install and configure the cb-event-forwarder, perform these steps as "root" on your target Linux system.

1. Install the CbOpenSource repository if it isn't already present:

    ```
    cd /etc/yum.repos.d
    curl -O https://opensource.carbonblack.com/release/x86_64/CbOpenSource.repo
    ```
2. Install the RPM via YUM: 

   ```
   yum install cb-event-forwarder
   ```

### Configure the cb-event-forwarder

Note: The 4.X event forwarder uses Yaml files for configuration. Values will be interpreted as their literal types, rather than using 0,1,T/F to represent booleans `true` or `false` should be used.

1. If installing on a machine *other than* the Cb Response server, copy the RabbitMQ username and password into the 
`rabbit_mq_username` and `rabbit_mq_password` variables in `/etc/cb/integrations/event-forwarder/cb-event-forwarder.conf` 
file. Also fill out the `cb_server_hostname` with the hostname or IP address where the Cb Response server can be reached.
If the cb-event-forwarder is forwarding events from a Cb Response cluster, the `cb_server_hostname` should be set
to the hostname or IP address of the Cb Response master node.
   
2. Ensure that the configuration is valid by running the cb-event-forwarder in Check mode: 
`/usr/share/cb/integrations/event-forwarder/cb-event-forwarder -check` as root. If everything is OK, you will see a 
message starting with "Initialized output”. If there are any errors, those errors will be printed to your screen.

### Configure Cb Response

By default, Cb publishes the `feed.*` and `watchlist.*` events over the bus (see the [Events documentation](EVENTS.md)
for more information). 
If you want to capture raw sensor events or the `binaryinfo.*` notifications, you have to enable those features in 
`/etc/cb/cb.conf`:

* If you are capturing raw sensor events then you also need to edit the `DatastoreBroadcastEventTypes` option in 
`/etc/cb/cb.conf` to enable broadcast of the raw sensor events you wish to export.
* If you are capturing binary observed events you also need to edit the `EnableSolrBinaryInfoNotifications` option in 
`/etc/cb/cb.conf` and set it to `True`.

Cb Response needs to be restarted if any variables were changed in `/etc/cb/cb.conf` by executing
`service cb-enterprise restart`. 

If you are configuring the cb-event-forwarder on a Cb Response cluster, the `DatastoreBroadcastEventTypes` and/or
`EnableSolrBinaryInfoNotifications` settings
must be distributed to the `/etc/cb/cb.conf` configuration file on all minion nodes and the cluster stopped and started using
the `/usr/share/cb/cbcluster stop && /usr/share/cb/cbcluster start` command.

### Starting and Stopping the Service

Once the service is installed, it is managed by the Upstart init system in CentOS 6.x. You can control the service via the 
initctl command.
* To start the service, `initctl start cb-event-forwarder`
* To stop the service, `initctl stop cb-event-forwarder`

Once the service is installed, it is configured to start automatically on system boot.

## Splunk

The Cb Response event forwarder can be used to export Cb Response events in a way easily configured for Splunk.  You'll
need to install and configure the Splunk TA to consume the Cb Response event data.   It is recommended that the event
bridge use a file based output with Splunk universal forwarder configured to monitor the file.   

More information about configuring the Splunk TA can be found [here](http://docs.splunk.com/Documentation/AddOns/latest/Bit9CarbonBlack/About)

## QRadar

The Cb Response event forwarder can forward Cb Response events in the LEEF format to QRadar. To forward Cb Response
events to a QRadar server:

1. Modify `/etc/cb/integrations/event-forwarder/cb-event-forwarder.conf` to include 
`udpout: "<qradaripaddress>:<port>"` (NOTE: Port is usually 514)
2. Change the output format to LEEF in the configuration file: `output_format: leef`.
3. Change the output type to UDP in the configuration file: `output_type: udp`.

For more information on the LEEF format, see the [Events documentation](EVENTS.md).

## Logging & Diagnostics

The connector logs to the directory `/var/log/cb/integrations/cb-event-forwarder`. An example of a successful startup log:

```
2015/12/07 12:57:26 cb-event-forwarder version 3.0.0 starting
2015/12/07 12:57:26 Interface address 172.22.10.7
2015/12/07 12:57:26 Interface address fe80::20c:29ff:fe85:bcd0
2015/12/07 12:57:26 Configured to capture events: [watchlist.hit.# watchlist.storage.hit.# feed.ingress.hit.# 
feed.storage.hit.# feed.query.hit.# alert.watchlist.hit.# ingress.event.process ingress.event.procstart 
ingress.event.netconn ingress.event.procend ingress.event.childproc ingress.event.moduleload 
ingress.event.module ingress.event.filemod ingress.event.regmod binaryinfo.# binarystore.file.added]
2015/12/07 12:57:26 Initialized output: File /var/cb/data/event_bridge_output.json
2015/12/07 12:57:26 Diagnostics available via HTTP at http://cbtest:33706/debug/vars
2015/12/07 12:57:26 Starting AMQP loop
2015/12/07 12:57:26 Connecting to message bus...
2015/12/07 12:57:26 Subscribed to watchlist.hit.#
2015/12/07 12:57:26 Subscribed to watchlist.storage.hit.#
2015/12/07 12:57:26 Subscribed to feed.ingress.hit.#
2015/12/07 12:57:26 Subscribed to feed.storage.hit.#
2015/12/07 12:57:26 Subscribed to feed.query.hit.#
2015/12/07 12:57:26 Subscribed to alert.watchlist.hit.#
2015/12/07 12:57:26 Subscribed to ingress.event.process
2015/12/07 12:57:26 Subscribed to ingress.event.procstart
2015/12/07 12:57:26 Subscribed to ingress.event.netconn
2015/12/07 12:57:26 Subscribed to ingress.event.procend
2015/12/07 12:57:26 Subscribed to ingress.event.childproc
2015/12/07 12:57:26 Subscribed to ingress.event.moduleload
2015/12/07 12:57:26 Subscribed to ingress.event.module
2015/12/07 12:57:26 Subscribed to ingress.event.filemod
2015/12/07 12:57:26 Subscribed to ingress.event.regmod
2015/12/07 12:57:26 Subscribed to binaryinfo.#
2015/12/07 12:57:26 Subscribed to binarystore.file.added
2015/12/07 12:57:26 Starting 4 message processors
```

In addition to the log file, the service starts an HTTP service for monitoring and debugging. The URL is available in 
the log file (see the “Diagnostics available” line above). The port is configurable through the `http_server_port`
option in the configuration file.

The diagnostics are presented as a JSON formatted string. The diagnostics include operational information on the
service itself, how long the service has been running, errors, and basic configuration information. An example
output from the JSON status is shown below:

```
{
  "version": "3.0.0",
  "uptime": 145.617786697,
  "cmdline": [
    "/usr/share/cb/integrations/event-forwarder/cb-event-forwarder",
    "/etc/cb/integrations/event-forwarder/cb-event-forwarder.conf"
  ],
  "connection_status": {
    "uptime": 145.471995845,
    "last_error_time": "0001-01-01T00:00:00Z",
    "last_error_text": "",
    "last_connect_time": "2015-12-08T00:22:56.566600876-05:00",
    "connected": true
  },
  "error_count": 0,
  "input_event_count": 29,
  "memstats": {...},
  "output_event_count": 29,
  "output_status": {
    "type": "file",
    "format": "json",
    "file:/var/cb/data/event_bridge_output.json": {
      "file_name": "/var/cb/data/event_bridge_output.json",
      "last_open_time": "2015-12-08T00:22:56.430385291-05:00"
    }
  },
  "subscribed_events": [
    "watchlist.hit.#",
    "watchlist.storage.hit.#",
    "feed.ingress.hit.#",
    "feed.storage.hit.#",
    "feed.query.hit.#",
    "alert.watchlist.hit.#",
    "ingress.event.process",
    "ingress.event.procstart",
    "ingress.event.netconn",
    "ingress.event.procend",
    "ingress.event.childproc",
    "ingress.event.moduleload",
    "ingress.event.module",
    "ingress.event.filemod",
    "ingress.event.regmod",
    "binaryinfo.#",
    "binarystore.file.added"
  ]
}
```

## Building from source

It is recommended to use golang 1.6.4.

Setup your GOPATH environment variable.
See [https://golang.org/doc/code.html#GOPATH](https://golang.org/doc/code.html#GOPATH) for details
```
go get github.com/carbonblack/cb-event-forwarder
go get -u github.com/golang/protobuf/proto
go get -u github.com/golang/protobuf/protoc-gen-go
go generate ./...
go get ./...
go build
```

## Event Filtering

The 4.X event forwarder supports custom filter defintions via golang's templating language to filter the stream of Cb Event Messages coming over the event-forwarder.

```
filter:
    enabled: true
    template: >-
              {{if (eq .type "alert.watchlist.hit.query.binary") -}}
                KEEP
              {{- else -}}
                DROP
              {{- end}}
```

##Encoders / Formating Events with Templates

The 4.X event forwarder supports custom event formatting defintions using golang's templating language to format the stream of Cb Event Messages coming over the event-forwarder based on template configured by the user.

Below is an example using the yaml syntax >- to open a whitespace clipped multi-line string sans-quotes. 

```
output_format: template
encoder:
    template: >- 
              {{$cbmsg := . }}
              {{- JsonFormat $cbmsg -}  
```

The templates involve the 
## Changelog

This connector has been completely rewritten for version 4.0.0 for greatly enhanced reliability, performance, customization and deployment at scale. 
See the [releases page](https://github.com/carbonblack/cb-event-forwarder/releases) 
for more information on new features introduced with each new version and upgrading from cb-event-forwarder 4.x.
