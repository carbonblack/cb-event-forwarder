# Carbon Black Forwarder Integration

## Overview

Carbon Black Forwarder Integration is a standalone service that is meant to provide event exporting capabilities to the Carbon Black Service Bus.

There are five output types that are supported

* STDOUT (used if no output type is specified)
* File
* TCP
* UDP
* S3

Events are sanitized up before we forward them, so that no sensitive information is exposed.

Output type as well as many other things are configured in the configuration file.

# Dependencies

Carbon Black Forwarder Integration depends on python-cb-integration greater than 5.1.0.15135 version

Carbon Black Enterprise has to be present on the machine where Forwarder Integration is installed.

# Installation and startup

Once you get the rpm package installed make sure you update the config file with all necessary credentials.
Bus Connection options are required for the service to start.

After configuration is done the service can be started with 'service cb-forwarder-bridge start' command

# Configuration

Configuration file is located at /usr/share/cb/integrations/cbforwarderbridge/

If 'file' output type is chosen there is a daily rotation for output files.
If 's3' output type is chosen then aws credentials need to be provided.

# Version

Current version is 5.1.0.150618