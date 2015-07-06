# Carbon Black Splunk Connector

## Overview

Carbon Black Splunk Connector is a standalone service that will listen on the Carbon Black enterprise bus and export
events in a normalized JSON format that is easily consumed by the Splunk forwarder in a format understood by the Splunk
TA.

The connector can be configured to capture different events from the event bus and export those into the JSON format.
By default all feed and watchlist hits, alerts, binary notifications, and raw sensor events are exported into JSON.  The
configuration file for the connector is stored in /etc/cb/integrations/cbsplunkbridge.conf.

The connector can be configured to output the events into various different output sources, including TCP, UDP, a file,
standard out, and Amazon S3.  For integration with the Splunk TA it is recommended that a Spunk TCP forwarder be used
and the events be output to a TCP socket.   An alternative approach would be to use the Splunk file monitor and have the
connector output the JSON to a file.

## Quickstart Guide

* Install the rpm for the Splunk connector "yum install cbsplunkbridge"
* Note: for EA testers the you'll need to install from a local file "rpm -ivh cbsplunkbridge-1.0.0.0-1.x86_64.rpm".
   You'll also need to install python-cb-integrations rpm.
* Decide and configure which events you want to forward from the event bus.   See the configuration file for more information.
* If you are capturing raw sensor events then you also need to edit the "DatastoreBroadcastEventTypes" option in the cb.conf 
   to enable broadcast of the raw sensor events you wish to export.
* If you are capturing binary observed events you also need to edit the "EnableSolrBinaryInfoNotifications" setting it to "True".
* Decide and configure the output type for the connector.   More information can be found in the configuration file.
* You'll need a service account that can be used by the service to query sensor information from within CB.   Set the "cbapi_token" 
  configuration option to the token of the service account to use.
* Configure your Splunk forwarder to consume the events given the specified output.
* Start the service with "service cb-splunk-bridge start".

## Dependencies

You must install the pika python package (http://pika.readthedocs.org/en/latest/).

Carbon Black Splunk Integration depends on python-cb-integration greater than 5.1.0.15135 version

Carbon Black Enterprise has to be present on the machine where Splunk Integration is installed.

The connector requires an API token in order to query sensor information from the Carbon Black server to populate with each
JSON event.   That token should be set in the "cbapi_token" configuration value.

## Configuration

Configuration file is located at /etc/cb/integrations/cbsplunkbridge/cbsplunkbridge.conf.  It contains detailed information
about each configuration option.

## Logging

The connector logs to the directory /var/log/cb/integrations/cbsplunkbridge.

## Version

Current version is 5.1.0.150618