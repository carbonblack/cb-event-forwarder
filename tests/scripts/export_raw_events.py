#!/usr/bin/env python

import collections
import os
import re
import sys
import optparse
import pika

sensorid_to_details_map = {}
cbapi = {}
g_output = None

g_config = {}


class EventOutput(object):
    DESTINATIONS = ['udp', 'tcp', 'file', 'stdout', 's3']

    def __init__(self, out_dest):

        if out_dest not in EventOutput.DESTINATIONS:
            raise ValueError("output destination (%s) not a valid destination value" % out_dest)

        self.dest = out_dest

    def output(self, mime_type, routing_key, eventdata, header_frame):
        raise Exception("Not Implemented")


class RawEventOutput(EventOutput):
    def __init__(self, outdir):
        super(RawEventOutput, self).__init__('file')
        self.destdir = outdir
        self.types = []

        os.makedirs(outdir, 0700)

        self.count = 0

    def output(self, mime_type, routing_key, eventdata, header_frame):
        if mime_type not in self.types:
            os.mkdir(os.path.join(self.destdir, mime_type), 0700)
            self.types.append(mime_type)

        open(os.path.join(self.destdir, mime_type,
                          "%d.%s" % (self.count, mime_type)),
             'wb').write(eventdata)
        open(os.path.join(self.destdir, mime_type, "%d.txt" % self.count), 'wb').write(str(header_frame))
        self.count += 1

    def get_stats(self):
        return self.count


def get_mq_user_from_cbconf():
    for line in open('/etc/cb/cb.conf').readlines():
        if line.strip().startswith('RabbitMQUser'):
            return line.split('=')[1].strip()


def get_mq_pass_from_cbconf():
    for line in open('/etc/cb/cb.conf').readlines():
        if line.strip().startswith('RabbitMQPassword'):
            return line.split('=')[1].strip()


def on_bus_msg(channel, method_frame, header_frame, body):
    """callback that gets called for any event on the CB pub/sub event bus"""

    try:
        if not header_frame.content_type.startswith("application/"):
            sys.stderr.write("->  Unexpected data type %s\n" % header_frame.content_type)
            sys.stderr.flush()
        else:
            g_output.output(header_frame.content_type.replace("application/", ""), method_frame.routing_key, body, header_frame)
    except Exception, e:
        sys.stderr.write("-> Exception processing bus msg: %s\n" % e)


def bus_event_loop(cb_hostname, rabbit_mq_user, rabbit_mq_pass):
    credentials = pika.PlainCredentials(rabbit_mq_user, rabbit_mq_pass)
    parameters = pika.ConnectionParameters(cb_hostname,
                                           5004,
                                           '/',
                                           credentials)

    connection = pika.BlockingConnection(parameters)
    channel = connection.channel()

    queue_name = 'raw_event_exporter_pid_%d' % os.getpid()

    # make sure you use auto_delete so the queue isn't left filling
    # with events when this program exists.
    channel.queue_declare(queue=queue_name, auto_delete=True)
    channel.exchange_declare(type='fanout', exchange='api.rawsensordata', durable=True, auto_delete=False,
                             passive=False)
    channel.queue_bind(exchange='api.rawsensordata', queue=queue_name)

    channel.basic_consume(on_bus_msg, queue=queue_name, no_ack=True)

    sys.stderr.write("-> Subscribed to Pub/Sub bus (press Ctl-C to quit)\n")
    sys.stderr.flush()

    try:
        channel.start_consuming()
    except KeyboardInterrupt:
        channel.stop_consuming()

    connection.close()


def build_cli_parser():
    parser = optparse.OptionParser(usage="%prog [options]", description="Process Cb Response Sensor Event Logs")

    #
    # CB server info (needed for host information lookups)
    #
    group = optparse.OptionGroup(parser, "CB server options")
    group.add_option("-c", "--cburl", action="store", default=None, dest="url",
                      help="CB server's URL. e.g., http://127.0.0.1; only useful when -A is specified")
    group.add_option("-a", "--apitoken", action="store", default=None, dest="token",
                      help="API Token for Cb Response server; only useful when -A and -c are specified")
    group.add_option("-n", "--no-ssl-verify", action="store_false", default=True, dest="ssl_verify",
                      help="Do not verify server SSL certificate; only useful when -c is specified.")
    parser.add_option_group(group)

    #
    # Bus options
    #
    group = optparse.OptionGroup(parser, "CB Bus connection options")
    group.add_option("-u", "--user", action="store", default=None, dest="user",
                      help="The username for the rabbitMQ pub/sub event bus (default is to pull it from config)")
    group.add_option("-p", "--pass", action="store", default=None, dest="pwd",
                      help="The password for the rabbitMQ pub/sub event bus (default is to pull it from config)")
    parser.add_option_group(group)

    #
    # Output options (ie - where do we put the formatted events and how are they formatted)
    #
    group = optparse.OptionGroup(parser, "Output options",
                                 "Output options for events that control both the formatting and destination")
    group.add_option("-d", "--directory", action="store", default=None, dest="outdir",
                      help="Write the raw events to a directory")
    parser.add_option_group(group)
    return parser


if __name__ == '__main__':
    parser = build_cli_parser()
    opts, args = parser.parse_args(sys.argv)

    cbhost = None

    if opts.url:
        cbapi['url'] = opts.url
        hostmatch = re.compile('https?://([^/]+)/?').match(opts.url)
        if hostmatch:
            cbhost = hostmatch.group(1)

    if not opts.outdir:
        parser.error("Output directory is required")

    # output processing
    g_output = RawEventOutput(opts.outdir)

    user = opts.user
    pwd = opts.pwd

    if not user:
        user = get_mq_user_from_cbconf()

    if not pwd:
        pwd = get_mq_pass_from_cbconf()

    if not cbhost:
        cbhost = 'localhost'

    bus_event_loop(cbhost, user, pwd)

