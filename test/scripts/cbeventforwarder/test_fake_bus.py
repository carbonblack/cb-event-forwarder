'''

A fake minimal Pika like channel to test the
spunk event listener


'''

import fnmatch
import gzip
import os
import struct
import time


class FakeFrame():

    def __init__(self, content_type, routing_key):

        self.routing_key = routing_key
        self.content_type = content_type
        self.delivery_tag = None


class FakeChannel():

    def __init__(self, callback, options, logger):
        self.callback = callback
        self.options = options
        self.logger = logger
        self.stop = False

        if 'test_eventlog_data' not in options:
            self.logger.error('Failed to find test_eventlog_data in config')
            return

        self.logger.info("Using FakeChannel test bus generator")

        test_dir = options.get('test_eventlog_data', None)

        if (test_dir is not None):
            self.logger.debug("Using test data setting: %s" % test_dir)

            # if they gave us a real path - read event logs and pump those through
            # the system
            if (os.path.exists(test_dir)):
                self.run_with_event_logs(options['test_eventlog_data'])
            else:
                # can't find the file - replay a few static events over and over
                self.run_saved_events()

    def run_saved_events(self):

        num_events = 0
        callback_time = 0
        callback_ct = 0

        # a serialized protobuf taken from a real event
        # log
        event = '\n+\x08\x04 \xba\xf2\x81\xbf\xd2\xfb\xde\xe7\x01('\
                '\xc3\xc2\xa2\x93\xa6\xa1\xb1\xd9t0\xab\xe2\x98\xba'\
                '\xae\x95\xf0\xb1\xab\x01H\xfb\xa5\xdd\xa9\x94\xbd'\
                '\xde\xe7\x01\x12.\x08\xab\xe2\x98\xba\xae\x95\xf0'\
                '\xb1\xab\x01\x12\x1fc:\\windows\\system32\\sru\\sru.log'\
                '\x18\x01*\x0c\x08\xc7\xdc\xa8\x89\xd1\xfd\xb0\xa3I\x10\x01'

        frame = FakeFrame('application/protobuf', 'ingress.event.process')

        while(self.stop is not True):
            num_events += 1

            start = time.time()

            self.callback(self, frame, frame, event)

            callback_time += time.time() - start
            callback_ct += 1

        self.logger.info("Qutting test logs due to stop_consuming() call")
        self.logger.debug("Average callback time %f" % (callback_time / callback_ct))

    def read_events_from_log(self, filename):
        self.logger.debug("Generating Fake bus events from file %s ...\n" % (filename))

        if filename[-3:] == '.gz':
            f = gzip.open(filename, 'rb')
        else:
            f = open(filename, 'rb')

        events = []

        while True:
            cb = f.read(4)
            if len(cb) < 4:
               break
            cb = struct.unpack('i', cb)[0]
            msg = f.read(cb)
            events.append(msg)

            self.logger.debug("%r" % msg)

        return events

    def find_logs(self, test_dir):

        found_logs = []

        for root, dir, files in os.walk(test_dir):
            for f in fnmatch.filter(files, '*.log.gz'):
                found_logs.append(os.path.join(root, f))
            for f in fnmatch.filter(files, "*.log"):
                found_logs.append(os.path.join(root, f))

        return found_logs

    def run_with_event_logs(self, test_dir):

        logs = self.find_logs(test_dir)
        num_logs = 0
        num_events = 0

        callback_ct = 0.0
        callback_time = 0.0

        for l in logs:

            num_logs += 1

            events = self.read_events_from_log(l)

            for e in events:

                #generate a fake bus message
                frame = FakeFrame('application/protobuf', 'ingress.event.process')
                num_events += 1

                start = time.time()

                self.callback(self,frame, frame, e)

                callback_time += time.time() - start
                callback_ct += 1

                if (self.stop):
                    self.logger.info("Qutting test logs due to stop_consuming() call")
                    self.logger.debug("Average callback time %f" % (callback_time / callback_ct))
                    return

        self.logger.info("All done running events into fake bus (logs: %d, events: %d)" % (num_logs, num_events))
        self.logger.debug("Average callback time %f" % (callback_time / callback_ct))

        while(self.stop is False):
            time.sleep(.1)

    def stop_consuming(self):
        # not sure what we do here
        self.stop = True

    def basic_ack(selfd, delivery_tag=None, multiple=True):
        # define this just to prevent things from breaking
        pass


class FakeConnection():

    def __init__(self):
        pass

    def close(self):
        pass
