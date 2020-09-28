# Output Formats Supported by the Event Forwarder

The VMware Carbon Black EDR Event Forwarder provides a way to easily connect arbitrary tools, applications, SIEMs, and analytics
to the [EDR message bus](https://developer.carbonblack.com/reference/enterprise-response/message-bus/). 
The Event Forwarder helps by providing the most critical data points from each event
in a consistent output format (currently JSON or LEEF) over a standard transport mechanism (currently a flat file,
TCP/UDP socket, Amazon S3 bucket, syslog or TCP+TLS encrypted syslog). This document describes the fields that the Event 
Forwarder appends to the input when generating the output JSON or LEEF data.

The Event Forwarder performs this normalization because, for performance reasons, two different data formats are used
for incoming events on the EDR message bus: 
[Google Protocol Buffers (or protobufs)](https://developers.google.com/protocol-buffers/) and [JSON](http://json.org).
Raw sensor events, such as file modification events and module load events, are sent on the bus as protobufs as they
require very high performance due to the very high event rate (which can reach hundreds of thousands of events per
second on a busy server). While we publish the current [protobuf definition](https://github.com/carbonblack/cbapi/blob/master/server_apis/proto/sensor_events.proto)
associated with the latest publicly released version of EDR, the protobuf definition, the event format itself,
and even the message bus transport mechanism are all subject to change in future releases of the EDR server.

More infrequent events, such as alerts or watchlist/feed hits, are sent as JSON over the event bus.

Therefore, the Event Forwarder provides a level of abstraction between the raw message bus and your application,
allowing you as the application developer or integrator to focus on adding value through data aggregation, correlation,
or analysis rather than the details of how to acquire the data in the first place.

## Output Format Normalization

The EDR Event Forwarder supports two output formats: JSON and LEEF.
No matter whether JSON or LEEF format is selected, some basic normalization is performed on the messages received
on the bus. Here are some highlights:

* The `timestamp` key contains the timestamp from the current event, which may be expressed relative to the 
  server clock (for server-generated events, such as feed/watchlist hits, etc.) or the endpoint's clock (for raw sensor
  events, such as netconns, filemods, etc.)
* For events that reference a specific process, the `process_guid` key contains the Process GUID for the associated
  process. Note that long lived or very busy processes (with more than 10,000 events) will be broken up into multiple
  process *segments*. The segment number is not known at ingest time for raw sensor events, so the GUID and associated
  deep link will point to the *first* segment for these processes (even if the event itself will be stored into a 
  later-numbered segment). If the segment number is known (for example, in a watchlist/feed/alert message), the 
  `segment_id` key contains the segment ID that contains the associated event.
* The `md5` key contains the MD5 hash of the process binary, module, or IOC associated with the event.
* The `type` key contains the internal message type from the bus: for example, `ingress.event.netconn` or 
  `watchlist.hit.process`.
* The `computer_name` key contains the hostname of the affected endpoint.

### EDR Deep Links

If configured, the EDR Event Forwarder will emit deep links into the EDR web UI for the relevant
sensor, binary, and/or process associated with a given event.
In addition, if the `cb_server_url` variable is set in the cb-event-forwarder configuration file, deep links will 
be created to the EDR web UI for the following keys. Note that only links that make sense for the given event
type (for example, there is no "child" process link for a process start event) will be included in the output:

* The `link_process` key contains a direct link to the associated process (and segment, if known). Note that
  raw sensor events do not contain a `segment_id` so the process link emitted with netconn, filemod, regmod, and other 
  raw sensor events will point to the *first* segment of the process, and not necessarily to the segment containing
  the event described in the output message.
* The `link_parent` key contains a direct link to the associated parent process.
* The `link_child` key contains a direct link to the associated child process.
* The `link_sensor` key contains a direct link to the associated endpoint host record in the EDR UI.
* The `link_md5`, `link_process_md5`, or `link_parent_md5` keys contain direct links to binaries related to this event
  in the EDR UI.

### JavaScript Object Notation (JSON)

[JSON](http://json.org) is a lightweight 
[international data interchange standard](http://www.ecma-international.org/publications/files/ECMA-ST/ECMA-404.pdf)
originally derived as a subset of the JavaScript language. The EDR Event Forwarder generates JSON by default
as it is highly interoperable with a variety of different languages and is also easy to read by hand when necessary.
JSON is the data format required to send events to the EDR [Splunk app](https://splunkbase.splunk.com/app/3099/).

### QRadar Log Event Extended Format (LEEF)

The [LEEF](https://www.ibm.com/developerworks/community/wikis/form/anonymous/api/wiki/9989d3d7-02c1-444e-92be-576b33d2f2be/page/3dc63f46-4a33-4e0b-98bf-4e55b74e556b/attachment/a19b9122-5940-4c89-ba3e-4b4fc25e2328/media/QRadar_LEEF_Format_Guide.pdf)
format is supported by EDR Event Forwarder to interoperate with IBM QRadar.
Each LEEF log message consists of a header followed by a set of tab-separated key-value pairs ("event attributes").
A set of keys, defined in the [IBM QRadar LEEF Format Guide](https://www.ibm.com/developerworks/community/wikis/form/anonymous/api/wiki/9989d3d7-02c1-444e-92be-576b33d2f2be/page/3dc63f46-4a33-4e0b-98bf-4e55b74e556b/attachment/a19b9122-5940-4c89-ba3e-4b4fc25e2328/media/QRadar_LEEF_Format_Guide.pdf),
have special meaning to QRadar ("pre-defined event attributes"). The pre-defined event attributes help QRadar identify
fields that are shared across multiple log sources, such as IP addresses and usernames, so that these log files
can be correlated with each other.

The LEEF header generated by EDR Event forwarder contains the following information:

```
LEEF:1.0|CB|CB|5.1.0.150625.500|watchlist.hit.process
```

1. The LEEF version is always set to 1.0.
2. The Vendor is always set to CB.
3. The Product is always set to CB.
4. The Version is currently set to the version of the running Cb server, if available; otherwise it is set to 5.1.
5. The EventID is set to the value of the `type` key (see above in the Normalization section). Also see the 
   [detailed events documentation](EVENTS.md) for more information on the event types recognized and forwarded by
   the Event Forwarder.


The EDR Event Forwarder has special support for mapping certain fields in the event message bus into LEEF
pre-defined event attributes to support the log correlation and alerting features present in QRadar. The following
normalization is performed on incoming messages to map their attributes to QRadar's pre-defined event attributes:

QRadar attribute   | Original key                   | Description
-------------------|--------------------------------|-----------------------------------------------
`src`              | `local_ip` or `remote_ip`      | Netconn events: the "source" IP address is either the Local IP address (for outgoing network connections) or the Remote IP address (for incoming network connections)
`dst`              | `remote_ip` or `local_ip`      | same as above
`proto`            | `protocol`                     | Netconn events: protocol ID (6 for TCP, 17 UDP, etc)
`srcPort`          | `local_port` or `remote_port`  | Netconn events: the "source" port is either the Local port (for outgoing network connections) or the Remote port (for incoming network connections)
`dstPort`          | `remote_port` or `local_port`  | same as above

