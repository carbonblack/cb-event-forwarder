# Carbon Black Event Forwarder Bridge

## Overview

Carbon Black Event Forwarder Bridge is a standalone service that will listen on the Carbon Black enterprise bus and export
events in a normalized JSON format.  These events can be consumed by any external system (SIEM), specifically Splunk.

The connector can be configured to capture different events from the event bus and export those into the JSON format.
By default all feed and watchlist hits, alerts, binary notifications, and raw sensor events are exported into JSON.  The
configuration file for the connector is stored in /etc/cb/integrations/cbeventbridge.conf.

The connector can be configured to output the events into various different output sources, including TCP, UDP, a file,
standard out, and Amazon S3.  

## Raw Sensor Events 

We have seen a performance impact when exporting all raw sensor events onto the enterprise bus.  We do not recommend
exporting all the events.  The performance impacts are seen when the events are broadcast on the bus, by enabling the
"DatastoreBroadcastEventTypes".  We recommend that at most, only process and netconn events be broadcast on the event
bus. 

## Quickstart Guide

* Install the rpm for the CB event bridge connector "yum install cbeventbridge"
* Note: for EA testers the you'll need to install from a local file "rpm -ivh cbeventbridge-5.1.0.150706.x86_64.rpm".
   You'll also need to install python-cb-integrations rpm.
* Decide and configure which events you want to forward from the event bus.   See the configuration file for more information.
* If you are capturing raw sensor events then you also need to edit the "DatastoreBroadcastEventTypes" option in the cb.conf 
   to enable broadcast of the raw sensor events you wish to export.
* If you are capturing binary observed events you also need to edit the "EnableSolrBinaryInfoNotifications" setting it to "True".
* Decide and configure the output type for the connector.   More information can be found in the configuration file.
* You'll need a service account that can be used by the service to query sensor information from within CB.   Set the "cbapi_token" 
  configuration option to the token of the service account to use.
* Start the service with "service cb-event-bridge start".

## Dependencies

You must install the pika python package (http://pika.readthedocs.org/en/latest/).

Carbon Black Splunk Integration depends on python-cb-integration greater than 5.1.0.15135 version

Carbon Black Enterprise has to be present on the machine where Splunk Integration is installed.

The connector requires an API token in order to query sensor information from the Carbon Black server to populate with each
JSON event.   That token should be set in the "cbapi_token" configuration value.

## Configuration

Configuration file is located at /etc/cb/integrations/cbeventbridge.conf.  It contains detailed information
about each configuration option.

## Splunk

The Carbon Black event forwarder can be used to export Carbon Black events in a way easily configured for Splunk.  You'll
need to install and configure the Splunk TA to consume the Carbon Black event data.   It is recommended that the event
bridge use a file based output with Splunk universal forwarder configured to monitor the file.   

## Logging

The connector logs to the directory /var/log/cb/integrations/cbeventbridge.

## Version

Current version is 5.1.0.150618