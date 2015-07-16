# Carbon Black Event Forwarder

## Overview

Carbon Black Event Forwarder is a standalone service that will listen on the Carbon Black enterprise bus and export
events in a normalized JSON format.  These events can be consumed by any external system that accepts JSON like Splunk and other SIEMs.

The connector can be configured to capture different events from the event bus and export those into the JSON format.
By default all feed and watchlist hits, alerts, binary notifications, and raw sensor events are exported into JSON.  The
configuration file for the connector is stored in `/etc/cb/integrations/event-forwarder/cb-event-forwarder.conf`.

The connector can be configured to output the events into various different output sources, including TCP, UDP, a file,
standard out, and Amazon S3.  

## Support

The pre-built RPM is supported via our [User eXchange (Jive)](https://community.bit9.com/groups/developer-relations) 
and via email to dev-support@bit9.com.  

## Raw Sensor Events 

We have seen a performance impact when exporting all raw sensor events onto the enterprise bus.  We do not recommend
exporting all the events.  The performance impacts are seen when the events are broadcast on the bus, by enabling the
"DatastoreBroadcastEventTypes".  We recommend that at most, only process and netconn events be broadcast on the event
bus. 

## Quickstart Guide

* Install the CbOpenSource repository if it isn't already present:
```
cd /etc/yum.repos.d
curl -O https://opensource.carbonblack.com/release/x86_64/CbOpenSource.repo
```
* Install the rpm for the CB event bridge connector: `yum install cb-event-forwarder`
* Decide and configure which events you want to forward from the event bus.   See the configuration file for more information.
* If you are capturing raw sensor events then you also need to edit the "DatastoreBroadcastEventTypes" option in the cb.conf 
   to enable broadcast of the raw sensor events you wish to export.
* If you are capturing binary observed events you also need to edit the "EnableSolrBinaryInfoNotifications" setting it to "True".
* Decide and configure the output type for the connector.   More information can be found in the configuration file.
* You'll need a service account that can be used by the service to query sensor information from within CB.   Set the "cbapi_token" 
  configuration option to the token of the service account to use.
* Start the service with `service cb-event-forwarder start`.

## Dependencies

Carbon Black Enterprise has to be present on the machine where this package is installed.  Installing it on the master for a cluster is the recommended approach -- you would NOT install this on every minion.

The connector requires an API token in order to query sensor information from the Carbon Black server to populate with each
JSON event.   That token should be set in the "cbapi_token" configuration value.

## Configuration

An example configuration file is located at `/etc/cb/integrations/event-forwarder/cb-event-forwarder.conf.example`. Copy the example to
  `/etc/cb/integrations/event-forwarder/cb-event-forwarder.conf` and edit it. It contains detailed information
about each configuration option.

## Splunk

The Carbon Black event forwarder can be used to export Carbon Black events in a way easily configured for Splunk.  You'll
need to install and configure the Splunk TA to consume the Carbon Black event data.   It is recommended that the event
bridge use a file based output with Splunk universal forwarder configured to monitor the file.   

## Logging

The connector logs to the directory `/var/log/cb/integrations/cb-event-forwarder`.

## Version

Current version is 1.1.0.
