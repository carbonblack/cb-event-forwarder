# Carbon Black Event Forwarder

## Overview

Carbon Black Event Forwarder is a standalone service that will listen on the Carbon Black enterprise bus and export
events (both watchlist/feed hits as well as raw endpoint events, if configured) in a normalized JSON or LEEF format.
The events can be saved to a file, delivered to a network service or archived automatically to an Amazon AWS S3 bucket.
These events can be consumed by any external system that accepts JSON or LEEF, including Splunk and IBM QRadar.

The list of events to collect is configurable.
By default all feed and watchlist hits, alerts, binary notifications, and raw sensor events are exported into JSON.  The
configuration file for the connector is stored in `/etc/cb/integrations/event-forwarder/cb-event-forwarder.conf`.

## Support

The pre-built RPM is supported via our [User eXchange (Jive)](https://community.bit9.com/groups/developer-relations) 
and via email to dev-support@bit9.com.  

## Raw Sensor Events 

We have seen a performance impact when exporting all raw sensor events onto the enterprise bus.  We do not recommend
exporting all the events.  The performance impacts are seen when the events are broadcast on the bus, by enabling the
"DatastoreBroadcastEventTypes".  We recommend that at most, only process and netconn events be broadcast on the event
bus. 

## Quickstart Guide

The cb-event-forwarder can be installed on any 64-bit Linux machine running CentOS 6.x. 
It can be installed on the same machine as the Carbon Black server, or another machine. 
If you are forwarding a large volume of events to QRadar (for example, all file modifications and/or registry 
modifications), then installing it on a separate machine is recommended. 
Otherwise, it is acceptable to install the cb-event-forwarder on the Carbon Black server itself.

### Installation

To install and configure the cb-event-forwarder, perform these steps as "root" on your target Linux system.

1. Install the CbOpenSource repository if it isn't already present:
    ```
    cd /etc/yum.repos.d
    curl -O https://opensource.carbonblack.com/release/x86_64/CbOpenSource.repo
    ```
2. Install the rpm for the CB event bridge connector: `yum install cb-event-forwarder`

### Configure the cb-event-forwarder
2. If installing on a machine *other than* the Carbon Black server, copy the RabbitMQ username and password into the 
`rabbit_mq_username` and `rabbit_mq_password` variables in `/etc/cb/integrations/event-forwarder/cb-event-forwarder.conf` 
file. Also fill out the `cb_server_hostname` with the hostname or IP address where the Carbon Black server can be reached.
3. Ensure that the configuration is valid by running the cb-event-forwarder in Check mode: 
`/usr/share/cb/integrations/event-forwarder/cb-event-forwarder -check` as root. If everything is OK, you will see a 
message starting with "Initialized output”. If there are any errors, those errors will be printed to your screen.

### Configure Carbon Black
By default, Cb publishes the `feed.*` and `watchlist.*` events over the bus (see the event descriptions & format document). 
If you want to capture raw sensor events or the `binaryinfo.*` notifications, you have to enable those features in 
`/etc/cb/cb.conf`:

* If you are capturing raw sensor events then you also need to edit the `DatastoreBroadcastEventTypes` option in 
`/etc/cb/cb.conf` to enable broadcast of the raw sensor events you wish to export.
* If you are capturing binary observed events you also need to edit the `EnableSolrBinaryInfoNotifications` option in 
`/etc/cb/cb.conf` and set it to `True`.

Carbon Black needs to be restarted if any variables were changed in `/etc/cb/cb.conf` by executing
`service cb-enterprise restart`. 

### Starting and Stopping the Service

Once the service is installed, it is managed by the Upstart init system in CentOS 6.x. You can control the service via the 
initctl command.
* To start the service, `initctl start cb-event-forwarder`
* To stop the service, `initctl stop cb-event-forwarder`

Once the service is installed, it is configured to start automatically on system boot.

## Splunk

The Carbon Black event forwarder can be used to export Carbon Black events in a way easily configured for Splunk.  You'll
need to install and configure the Splunk TA to consume the Carbon Black event data.   It is recommended that the event
bridge use a file based output with Splunk universal forwarder configured to monitor the file.   

More information about configuring the Splunk TA can be found [here](http://docs.splunk.com/Documentation/AddOns/latest/Bit9CarbonBlack/About)

## QRadar

The Carbon Black event forwarder can forward Carbon Black events in the LEEF format to QRadar. To enable LEEF
output: modify `/etc/cb/integrations/event-forwarder/cb-event-forwarder.conf` to include 
`udpout=<qradaripaddress>:<port>` (NOTE: Port is usually 514) and specify LEEF output format: `output_format=leef`. 

## Logging & Diagnostics

The connector logs to the directory `/var/log/cb/integrations/cb-event-forwarder`. An example of a successful startup log:

```
2015/12/07 12:57:26 cb-event-forwarder version 3.0.0 starting
2015/12/07 12:57:26 Interface address 172.22.10.7
2015/12/07 12:57:26 Interface address fe80::20c:29ff:fe85:bcd0
2015/12/07 12:57:26 Configured to capture events: [watchlist.hit.# watchlist.storage.hit.# feed.ingress.hit.# feed.storage.hit.# feed.query.hit.# alert.watchlist.hit.# ingress.event.process ingress.event.procstart ingress.event.netconn ingress.event.procend ingress.event.childproc ingress.event.moduleload ingress.event.module ingress.event.filemod ingress.event.regmod binaryinfo.# binarystore.file.added]
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
the log file (see the “Diagnostics available” line above).
