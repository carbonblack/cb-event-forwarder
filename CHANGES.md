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
  output is available on http://<hostname>:33706/debug/vars. Note that this port may have to be opened via iptables
  for it to be accessed remotely.