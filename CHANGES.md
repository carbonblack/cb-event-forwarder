# Changelog

## cb-event-forwarer 3.5.0
- kafka SASL support
- OATH2 JWT optional support for http output
- Support for sending EventText as bytearary httpoutput 

## cb-event-forwarder 3.1.2

The 3.1.2 release of cb-event-forwarder adds two features:
 
* You can now send arbitrary messages for debugging/testing purposes through the forwarder to the output location.
  This is only available when the cb-event-forwarder is started with the `-debug` command line switch. Messages
  sent via this mechanism are also logged for audit purposes.
* S3: You can now explicitly specify the location of the AWS credential file to use for authentication in the
  `credential_profile` option in the `[s3]` section of the configuration file. To search for the credential profile
  `production` in the credentials stored in the file `/etc/cb/aws.creds`, set the `credential_profile` option to
  `/etc/cb/aws.creds:production`.

## cb-event-forwarder 3.1.1

The 3.1.1 release of cb-event-forwarder fixes a critical bug when rolling over files. Previous versions of the
cb-event-forwarder would stop rolling over files after the first of a new month. This release fixes that bug.

## cb-event-forwarder 3.1.0

The 3.1.0 release of cb-event-forwarder adds the following features over 3.0.0:

* "Deep links" into the Cb server UI are now optionally available in the output
  * These links allow you to directly access the relevant sensor, binary, or process context for each event output
    by the cb-event-forwarder.
  * The new variable `cb_server_url` has been added to the configuration file to support this new feature. Set this
    variable to the base URL of the Carbon Black web UI. **If this variable is not set, then no links are generated.**
  * The new links are available in the `link_process`, `link_child` (in child process events), `link_md5` and 
    `link_sensor` keys of the JSON or LEEF output.
  * Note that links to processes and binaries may result in 404 errors until the process and binary data is committed
    to disk on the Carbon Black server. Process events received via the event-forwarder may take up to 15 minutes or 
    longer before they're visible on the Carbon Black web UI.
* All Carbon Black 5.1 event types are now supported
  * Microsoft EMET
  * Carbon Black Tamper events
  * Cross-process (process open/thread create) events
  * Carbon Black process/network blocking events
* Network events now include the local IP and port number of the network connection (available on Carbon Black 5.1 
  servers and sensors)
  * The IP four-tuple is now available as (`local_ip`, `local_port`, `remote_ip`, and `remote_port`) in the JSON/LEEF
    output
* Provide a human-readable status page for statistics
  * By default, these statistics are available via HTTP on port 33706 of the system running the cb-event-forwarder.
* Fix regressions on output from cb-event-forwarder 2.x on some JSON message types
  * cb-event-forwarder 3.0.0 was missing the `computer_name` field from some JSON messages
* New Amazon S3 options; see the `[s3]` section of the configuration file
  * Specify whether the files uploaded to S3 should be encrypted with server-side encryption (see `server_side_encryption`)
  * Define an ACL policy to apply to files uploaded to S3 (see `acl_policy`)
  * Specify the credential profile used when connecting to S3 (see `credential_profile`)

# Changes from the cb-event-forwarder 2.x to 3.x

In general, the new cb-event-forwarder 3.0 is designed to be a drop-in replacement for previous versions of the
event forwarder. There are a few bug fixes, configuration changes and enhancements of note. The most important change
is that the service is now managed by the "upstart" system in CentOS 6. The `service` command is no longer used to
control the service; instead use `start cb-event-forwarder` and `stop cb-event-forwarder` to manually start and stop
the service.

## Configuration

The configuration file location still defaults to `/etc/cb/integrations/event-forwarder/cb-event-forwarder.conf` and
most existing configuration files will work unchanged with this new version. 
The following changes have been made to the configuration file in version 3.0:

* The S3 output now expects the AWS credentials to be placed in the AWS standard locations for the API. **The 
  `aws_key` and `aws_secret` options are now ignored.**
  * You can use `aws configure` to configure them interactively
  * The environment variables `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, etc.
  * The file `~/.aws/credentials` on Linux and Mac OS X
  * Amazon EC2 instances may use the EC2 metadata service
  * See http://docs.aws.amazon.com/cli/latest/userguide/cli-chap-getting-started.html for more information.
  
* The S3 output now supports changing the region and temporary directory from the `s3out` configuration option.
  * `s3out=(temp-file-directory):(region):(bucket-name)`

* There is a new option, `http_server_port` which defaults to 33706.
  * This port is opened on the system running the cb-event-forwarder to report back status information. See the README
    for more information on the status report.

* The `message_processor_count` configuration option is now ignored.
  * The number of message processors is automatically set to twice the number of CPU cores available when the 
    cb-event-forwarder starts.

* There is a new option, `output_format` for switching between LEEF and JSON output formats
  * The LEEF output format is optimized for IBM QRadar
  
* The `stdout` output option has been removed.

## Output format

* The `tcp` output now places a newline (`\r\n`) between each event in the output stream

* Bugfix: the output from the `childproc` event type now contains the correct `process_guid` value

* Bugfix: the output from the `procend` event type now contains the MD5 from the process that exited in the `md5` value

## Operations

* The daemon is now managed by the "upstart" system in CentOS 6.
  * Use the `start` and `stop` commands to control the daemon: `start cb-event-forwarder`.

* The daemon now supports the `SIGHUP` signal.
  * When configured with a `file` output, `SIGHUP` will immediately roll over the event file
  * When configured with an `s3` output, `SIGHUP` will immediately roll over the current log and flush the logs to S3

* The cb-event-forwarder now starts an HTTP server on port 33706 with configuration and status reporting. A raw JSON
  output is available on http://yourhost:33706/debug/vars. Note that this port may have to be opened via iptables
  for it to be accessed remotely.
