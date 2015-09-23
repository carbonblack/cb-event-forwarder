import datetime
import logging
import multiprocessing
import os
import random
import socket
import stat
import string
import sys
import time
import json
import boto

from event_parser import EventParser

LOGGER = logging.getLogger(__name__)


class EventOutput(object):
    OUTPUT_TYPES = ["udp", "tcp", "file", "stdout", "s3"]

    def __init__(self, output_type):

        if output_type not in EventOutput.OUTPUT_TYPES:
            raise ValueError("output type (%s) not valid" % output_type)

        self.output_type = output_type

    def write(self, event_data):
        raise Exception("Not Implemented")


class StdOutOutput(EventOutput):
    def __init__(self):
        super(StdOutOutput, self).__init__("stdout")

    def write(self, event_data):
        sys.stdout.write(str(event_data) + "\n")


class FileOutput(EventOutput):
    def __init__(self, outfile):
        super(FileOutput, self).__init__("file")

        self.outfile = outfile
        self.lock = multiprocessing.Manager().Lock()

    def write(self, event_data):
        try:
            self.lock.acquire()
            self.prepare_file()
            fout = open(self.outfile, "a")
            fout.write(str(event_data) + "\n")
            fout.flush()
            fout.close()
            self.lock.release()
        except:
            LOGGER.exception("Unable to output event via File")

    def should_rollover(self):
        """
        Determine if rollover should occur. Rolling occurs at midnight local time
        """

        if os.path.exists(self.outfile):
            last_modified = datetime.datetime.fromtimestamp(os.stat(self.outfile)[stat.ST_MTIME])
            if last_modified.date() < datetime.datetime.now().date():
                return True
        return False

    def prepare_file(self):
        """
        Rollover the file if a rollover is required
        """

        if not self.should_rollover():
            return

        last_modified_date = datetime.datetime.fromtimestamp(os.stat(self.outfile)[stat.ST_MTIME])

        rolled_filename = self.outfile + "." + last_modified_date.strftime("%Y%m%d")
        if os.path.exists(rolled_filename):
            os.remove(rolled_filename)
        if os.path.exists(self.outfile):
            os.rename(self.outfile, rolled_filename)


class UdpOutput(EventOutput):
    def __init__(self, host, port):
        super(UdpOutput, self).__init__("udp")

        self.ip = socket.gethostbyname(host)
        self.port = port

    def write(self, event_data):
        try:
            sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
            sock.sendto(str(event_data), (self.ip, self.port))
        except:
            LOGGER.exception("Unable to output event via UDP")


class TcpOutput(EventOutput):
    def __init__(self, host, port):
        super(TcpOutput, self).__init__("tcp")

        self.ip = socket.gethostbyname(host)
        self.port = port

    def write(self, event_data):
        try:
            sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
            sock.connect((self.ip, self.port))
            sock.send(str(event_data))
            sock.close()
        except:
            LOGGER.exception("Unable to output event via TCP")


class S3Output(EventOutput):
    def __init__(self, bucket, key, secret):
        super(S3Output, self).__init__('s3')

        # s3 creds must be defined either in an environment variable, boto config
        # or EC2 instance metadata.
        self.conn = boto.connect_s3(aws_access_key_id=key, aws_secret_access_key=secret)
        self.bucket = self.conn.get_bucket(bucket)

    def write(self, event_data):
        # name keys as timestamp-xxxx where xxx is random 4 lowercase chars
        # this (a) keeps a useful and predictable sort order and (b) avoids name collisions
        key_name = "%s-%s" % (time.time(), ''.join(random.sample(string.lowercase, 4)))
        k = boto.s3.key.Key(bucket=self.bucket, name=key_name)

        # ensure contents of s3 bucket are list of objects, even though this output path is
        # always only one object per call
        k.set_contents_from_string("[%s]\n" % event_data)


class EventProcessor(object):
    def __init__(self, options):
        self.event_parser = EventParser(options)

        output_type = options.get("output_type")
        self.output = None
        if output_type == "stdout":
            self.output = StdOutOutput()
        elif output_type == "file":
            self.output = FileOutput(options.get("outfile"))
        elif output_type == "tcp":
            (host, port) = options.get("tcpout").split(":")
            self.output = TcpOutput(host, int(port))
        elif output_type == "udp":
            (host, port) = options.get("udpout").split(":")
            self.output = UdpOutput(host, int(port))
        elif output_type == "s3":
            self.output = S3Output(options.get("s3out"), options.get("aws_key"), options.get("aws_secret"))
        else:
            raise ValueError("Invalid output type: %s" % output_type)

    def process_event(self, content_type, routing_key, body):
        """
        Parse an event from the Carbon Black event bus and write the resulting events to the configured output
        """

        events = self.event_parser.parse_events(content_type, routing_key, body)
        for event in events:
            try:
                json_str = json.dumps(event)
                self.output.write(json_str)
            except TypeError as e:
                LOGGER.exception("Got an exception (%s) processing event (repr: %s)" % (e, repr(event)))
