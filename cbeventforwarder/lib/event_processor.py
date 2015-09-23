import datetime
import logging
import multiprocessing
import os
import socket
import sys
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
    def __init__(self, outfile, rollover_delta=None):
        super(FileOutput, self).__init__("file")

        self.outfile = outfile
        self.lock = multiprocessing.Manager().Lock()
        if rollover_delta:
            self.rollover_delta = datetime.timedelta(seconds=rollover_delta)
            self.rollover_at_midnight = False
        else:
            self.rollover_at_midnight = True
            self.rollover_delta = None

    def write(self, event_data):
        try:
            self.lock.acquire()
            rolled_over = self.prepare_file()
            fout = open(self.outfile, "a")
            fout.write(str(event_data) + "\n")
            fout.flush()
            fout.close()
            self.lock.release()

            return rolled_over
        except:
            LOGGER.exception("Unable to output event via File")
            return False

    def should_rollover(self):
        """
        Determine if rollover should occur.
        """

        if not os.path.exists(self.outfile):
            return False

        last_modified = datetime.datetime.fromtimestamp(os.path.getmtime(self.outfile))
        create_time = datetime.datetime.fromtimestamp(os.path.getctime(self.outfile))

        if self.rollover_at_midnight:
            if last_modified.date() < datetime.datetime.now().date():
                return True
        else:
            if datetime.datetime.now() - create_time > self.rollover_delta:
                return True

        return False

    def prepare_file(self):
        """
        Rollover the file if a rollover is required
        """

        if not self.should_rollover():
            return False

        last_modified_date = datetime.datetime.fromtimestamp(os.path.getmtime(self.outfile))

        rolled_filename = self.outfile + "." + last_modified_date.strftime("%Y%m%d")
        if os.path.exists(rolled_filename):
            os.remove(rolled_filename)
        if os.path.exists(self.outfile):
            os.rename(self.outfile, rolled_filename)

        return rolled_filename


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
    def __init__(self, bucket, key, secret, temp_file_location="/var/run/cb/integrations/event-forwarder",
                 bucket_time_delta=10):           # FIXME: time_delta set to 10s for testing
        super(S3Output, self).__init__('s3')

        # s3 creds must be defined either in an environment variable, boto config
        # or EC2 instance metadata.
        self.conn = boto.connect_s3(aws_access_key_id=key, aws_secret_access_key=secret)

        self.bucket = self.conn.get_bucket(bucket)
        self.bucket_time_delta = datetime.timedelta(seconds=bucket_time_delta) # by default, 5 minutes
        self.temp_file_location = temp_file_location

        self.file_output = FileOutput(os.path.join(temp_file_location, "event-forwarder"))

        # upload any files that failed to upload on previous instances of event-forwarder.
        self.upload_stragglers()
        self.last_uploaded_stragglers = datetime.datetime.now()

    def upload_stragglers(self):
        for fn in [x for x in os.listdir(self.temp_file_location)
                   if os.path.isfile(os.path.join(self.temp_file_location, x))]:
            path = os.path.join(self.temp_file_location, fn)
            self.upload_one(path)

    def upload_one(self, path):
        # TODO: make this a background task so we don't block event processing
        # if the file fails to upload for any reason, we will pick it up later in upload_stragglers()

        key_name = os.path.basename(path)
        original_file_size = os.path.getsize(path)
        try:
            k = boto.s3.key.Key(bucket=self.bucket, name=key_name)
            upload_size = k.set_contents_from_filename(path)
        except Exception as e:
            LOGGER.exception("Exception while uploading file %s" % path)
            return False
        else:
            if upload_size == original_file_size:
                os.remove(path)
                return True
            else:
                LOGGER.error("File size mismatch while uploading file %s: %d != %d" % (path,
                                                                                       upload_size,
                                                                                       original_file_size))
                return False

    def write(self, event_data):
        rolled_over = self.file_output.write(event_data)
        if rolled_over:
            # take the rolled_over file and upload it...
            filename = unicode(rolled_over)
            self.upload_one(filename)

        if datetime.datetime.now() - self.last_uploaded_stragglers > datetime.timedelta(seconds=60):
            self.upload_stragglers()
            self.last_uploaded_stragglers = datetime.datetime.now()


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
