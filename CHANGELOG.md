# CB EDR Event Forwarder Changelog

## v3.8.2

#### Features

* Support for dockerized EDR added via new event-forwarder docker image. 

#### Bug Fixes / Changes

* Adapt to change in RabbitMQ authentication in EDR v7.7.0, but maintain backwards compatibility. 
* Fixed issue that could cause event forwarder to lock up if excessive amount of time happened without logging events

## v3.8.1

#### Bug Fixes / Changes

* Fix a bug where timestamps were missing from process events
* Fix a bug where messages were silently dropped in certain error cases 
* Improve hostname detection
* Better error logging for configuration errors

## v3.8.0

#### Features

 * The new EDR event `task.error.logged` is now supported. This event is enabled using `task_errors=ALL` in the EF configuration file. It is also supported in the EDR console configuration page for EF, starting with EDR v7.5.0.
 * `compression_type=lz4` A new compression type, `lz4` is now available. The `gzip` type is still the default. LZ4 is a lossless data compression algorithm that is focused on compression and decompression speed.
* `log_level=DEBUG` can be used to control logging level

#### Bug Fixes / Changes

 * Log files are now retained and rolled over after restarts.
 * Under some circumstances the EF process would not terminate when shut down. This is no longer the case.
 * S3 output has been improved.
 * Protobuf processing performance improvements

## v3.7.6

#### Changes

* Adapt to change in RabbitMQ authentication in EDR v7.7.0, but maintain backwards compatibility.

## v3.7.5

#### Bug Fixes / Changes

 * Fixed a bug causing NetconnBlocked messages to be written out

## v3.7.4

#### Bug Fixes / Changes

 * Changed the startup permissions script to operate more gracefully on systems where EDR is not installed. 

## v3.7.3

#### Bug Fixes / Changes

 * Fixed an error in the HTTPOutput that would cause the forwarder to send empty-files to the remote even when not configured to do so.

## v3.7.2

#### Features

 * EDR Event Forwarder automatically sets cb_server_url and cb_server_name using the FQDN of the host it is running on , unless these options are explicitly set in the configuration file

#### Bug Fixes / Changes

 * HTTP Output handler detects errors at runtime, and will not allow configurations lacking a protocol-prefix like http(s)://

## v3.7.1

#### Features

 * EDR Event Forwarder continues to run during communication outages.  Previously, it would exit on timeout.

#### Bug Fixes / Changes

 * Corrected signal handling, permitting EDR Event Forwarder to continue to execute during communication outages.

## v3.7.0

#### Features

 * We now support Antimalware Scan Interface (AMSI) events. This event is called `ingress.event.filelessscriptload`. Please note that you will need EDR 7.2.0 in order to receive these events.
 * New command-line option `-pid-file <pid_filename>` for better parity with other services, and to facilitate process monitoring.

#### Bug Fixes / Changes

 * Reverted use of Confluent Kafka client library to the pure Go Sarama client.
 * Removed configuration settings `api_token`, `api_verify_ssl`, and `api_proxy_ssl`. Event Forwarder no longer needs to use the EDR API to perform event post-processing. EDR now has built-in capability for adding report titles to feed hit events.
 * Changed some log messages in the protobuf processing code to debug level, to avoid filling log files with unneeded entries.
 * Specify CA/Client cert/keys in PEM format.
 * Deprecate Upstart in favor of sysvinit for service control on EL6 systems

## v3.6.3

#### Features

 * Switched from the GZIP library to PGZIP for faster and more efficient compression.

#### Bug Fixes / Changes

 * The requirements of the `s3out` configuration setting have been relaxed such that 
 you may omit the leading "temp-file-directory" element. In other words, it is sufficient 
 to use the format `s3out=[region]:[bucket-name]`.
 
#### Related Changes in CB EDR

 * Corrected the CB EDR configuration page for the Event Forwarder to allow changing
 the "Max bundle size". Prior to this fix, submitting a configuration change with a
 new value for that setting resulted in a server error. **NOTE: this fix requires CB EDR
 version 7.2.0 or higher.**

## v3.6.2

#### Features

 * Event Forwarder can now be configured and operated from the CB EDR web console.
 * There are no new features in Event Forwarder itself.

#### Bug Fixes

 * Fix signal handling for syslog and S3 output types
 * Fix error handling for AMQP connections

## v3.6.1

### Features

 * CentOS/RHEL 7.x compatibility with separate packages for el6 and el7.
 * New metric support
 * Threading for Kafka output,
 * Ability to configure more options for kafka.

### Bug Fixes

 * Streamlined error reporting, removing superfluous and numerous
`blocked_netconn` exceptions from the event forwarder stream.

## v3.6.0

 * Overhaul support for Kafka output
 * Various fixes and support for compression in HTTP/S3 outputs.
 * Use the new `[kafka.producer]` section to specify arbitrary Kafka producer
 options based on the [Kafka producer API](https://docs.confluent.io/current/installation/configuration/producer-configs.html)
 for details on the supported configuration options. This allows for supporting
 Kafka producer TLS/SSL options, compression, and various others if desired.
 Continue to specify `output_type=kafka` and

 
     [kafka]
     brookers=comma-delimited-broker-list
     
in your configuration file to try things out.

## v3.5.0
 * Kafka SASL support
 * OATH2 JWT optional support for http output
 * Support for sending EventText as bytearary httpoutput 

## v3.1.2

 * You can now send arbitrary messages for debugging/testing purposes through the forwarder to the output location.
  This is only available when the cb-event-forwarder is started with the `-debug` command line switch. Messages
  sent via this mechanism are also logged for audit purposes.
 * S3: You can now explicitly specify the location of the AWS credential file to use for authentication in the
  `credential_profile` option in the `[s3]` section of the configuration file. To search for the credential profile
  `production` in the credentials stored in the file `/etc/cb/aws.creds`, set the `credential_profile` option to
  `/etc/cb/aws.creds:production`.

## v3.1.1

The 3.1.1 release of cb-event-forwarder fixes a critical bug when rolling over files. Previous versions of the
cb-event-forwarder would stop rolling over files after the first of a new month. This release fixes that bug.

## v3.1.0

 * "Deep links" into the CB server UI are now optionally available in the output
   * These links allow you to directly access the relevant sensor, binary, or process context for each event output
    by the cb-event-forwarder.
   * The new variable `cb_server_url` has been added to the configuration file to support this new feature. Set this
    variable to the base URL of the EDR web UI. **If this variable is not set, then no links are generated.**
   * The new links are available in the `link_process`, `link_child` (in child process events), `link_md5` and 
    `link_sensor` keys of the JSON or LEEF output.
   * Note that links to processes and binaries may result in 404 errors until the process and binary data is committed
    to disk on the EDR server. Process events received via the event-forwarder may take up to 15 minutes or 
    longer before they are visible on the EDR web UI.
* All EDR 5.1 event types are now supported
   * Microsoft EMET
   * EDR Tamper events
   * Cross-process (process open/thread create) events
   * EDR process/network blocking events
* Network events now include the local IP and port number of the network connection (available on EDR 5.1 
  servers and sensors)
   * The IP four-tuple is now available as (`local_ip`, `local_port`, `remote_ip`, and `remote_port`) in the JSON/LEEF
    output
* Provided a human-readable status page for statistics
   * By default, these statistics are available via HTTP on port 33706 of the system running the cb-event-forwarder.
* Fixed regressions on output from cb-event-forwarder 2.x on some JSON message types
   * cb-event-forwarder 3.0.0 was missing the `computer_name` field from some JSON messages
* New Amazon S3 options; see the `[s3]` section of the configuration file
   * Specify whether the files uploaded to S3 should be encrypted with server-side encryption (see `server_side_encryption`)
   * Define an ACL policy to apply to files uploaded to S3 (see `acl_policy`)
   * Specify the credential profile used when connecting to S3 (see `credential_profile`)

---

# Changes from v2.x to v3.x

In general, the new cb-event-forwarder 3.0 is designed to be a drop-in replacement for previous versions of the
event forwarder. There are a few bug fixes, configuration changes and enhancements of note. The most important change
is that the service is now managed by the "upstart" system in CentOS 6. The `service` command is no longer used to
control the service; instead use `start cb-event-forwarder` and `stop cb-event-forwarder` to manually start and stop
the service.

### Configuration

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

### Output format

* The `tcp` output now places a newline (`\r\n`) between each event in the output stream

* Bugfix: the output from the `childproc` event type now contains the correct `process_guid` value

* Bugfix: the output from the `procend` event type now contains the MD5 from the process that exited in the `md5` value

### Operations

* The daemon is now managed by the "upstart" system in CentOS 6.
  * Use the `start` and `stop` commands to control the daemon: `start cb-event-forwarder`.

* The daemon now supports the `SIGHUP` signal.
  * When configured with a `file` output, `SIGHUP` will immediately roll over the event file
  * When configured with an `s3` output, `SIGHUP` will immediately roll over the current log and flush the logs to S3

* The cb-event-forwarder now starts an HTTP server on port 33706 with configuration and status reporting. A raw JSON
  output is available on http://yourhost:33706/debug/vars. Note that this port may have to be opened via iptables
  for it to be accessed remotely.
