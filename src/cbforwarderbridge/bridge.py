#!/usr/bin/env python

import logging
import multiprocessing
import os
import pika
import sys
import time

from cbint.utils.daemon import CbIntegrationDaemon
from lib.event_processor import EventProcessor


def process_event(event_processor, event):
    """
    Processor pool entry point. Responsible for processing and writing one event from the message bus
    """

    log = logging.getLogger(__name__)
    try:
        log.debug("Processing Event: %s - %s" % (event["content_type"], event["routing_key"]))
        event_processor.process_event(event["content_type"], event["routing_key"], event["body"])
    except:
        log.exception("Unable to process event")


class CarbonBlackForwarderBridge(CbIntegrationDaemon):
    """
    Integration daemon for Carbon Black event forwarding to Forwarder
    """

    def __init__(self, name, configfile):
        CbIntegrationDaemon.__init__(self, name, configfile=configfile)

        self.forwarder_options = self.options.get("bridge")
        self.debug = False
        self.retry = 0
        self.max_retry = 100

        self.capture_events = None

        self.channel = None
        self.connection = None
        self.event_processor = None
        self.processor_pool = None
        self.testing = False

    def validate_config(self):
        """
        Validate service config:
            - output_type must be defined
            - rabbitmquser must be defined
            - rabbitmqpassword must be defined
            - RabbitMQ connection must succeed
        """

        if "bridge" in self.options:
            self.forwarder_options = self.options.get("bridge")
        else:
            self.logger.error("configuration does not contain a [bridge] section")
            return False

        config_valid = True
        msgs = []

        self.testing = 'test_eventlog_data' in self.forwarder_options

        output_msg = "%s needs to be set"

        if "output_type" not in self.forwarder_options:
            msgs.append(output_msg % "OutputType")
            config_valid = False

        # Testing connection
        if config_valid and not self.testing:
            try:
                username, password = self.get_bus_credentials()
                credentials = pika.PlainCredentials(username, password)
                parameters = pika.ConnectionParameters("localhost", 5004, "/", credentials)
                connection = pika.BlockingConnection(parameters)
                connection.close()
            except:
                msgs.append("Connection failed. Check configs for correct username and password")
                config_valid = False

        if not config_valid:
            for msg in msgs:
                sys.stderr.write("%s\n" % msg)
                self.logger.error(msg)
            return False
        else:
            return True

    def on_starting(self):
        """
        Determine which Carbon Black events should be sent to forwarder
        """

        self.set_capture_events_from_config()


    def run(self):
        """
        Prepare to process RabbitMQ events and start consuming
        """

        self.debug = self.forwarder_options.get("debug", "0") != "0"
        if self.debug:
            self.logger.setLevel(logging.DEBUG)

        processor_count = int(self.forwarder_options.get("message_processor_count", 1))
        cpu_count = multiprocessing.cpu_count()
        if processor_count > cpu_count:
            self.logger.info("processor_count (%s) > cpu_count. Defaulting to cpu_count", (processor_count, cpu_count))
            processor_count = cpu_count

        self.event_processor = EventProcessor(self.forwarder_options)
        self.processor_pool = multiprocessing.Pool(processor_count)

        self.consume_message_bus(test=self.testing)

    def on_stopping(self):
        """
        Close any open resources
        """

        try:
            if self.channel is not None:
                self.channel.stop_consuming()
                self.channel = None

            if self.connection is not None:
                self.connection.close()
                self.connection = None

            if self.processor_pool is not None:
                self.processor_pool.close()
                self.processor_pool.join()

            self.debug = self.forwarder_options.get("debug", "0") != "0"
            if self.debug:
                self.logger.setLevel(logging.DEBUG)
        except:
            self.logger.exception("Error stopping service")

    def consume_message_bus(self, test=False):
        """
        Subscribe to Carbon Black's event bus and begin consuming messages
        """

        if test:
            from test_fake_bus import FakeChannel, FakeConnection
            self.logger.info("Running Test Message Bus")

            self.channel = FakeChannel(self.on_bus_message, self.forwarder_options, self.logger)
            self.connection = FakeConnection()

            return

        self.logger.info("Subscribing to message bus")

        username, password = self.get_bus_credentials()
        credentials = pika.PlainCredentials(username, password)
        parameters = pika.ConnectionParameters("localhost", 5004, "/", credentials)
        queue_name = "forwarder_exporter_pid_%d" % os.getpid()

        try:
            self.connection = pika.BlockingConnection(parameters)
            self.channel = self.connection.channel()
            self.channel.queue_declare(queue=queue_name, auto_delete=True)
            self.channel.queue_bind(exchange="api.events", queue=queue_name, routing_key="#")
            self.channel.basic_consume(self.on_bus_message, queue=queue_name)

            self.logger.info("Consuming message bus")
            self.channel.start_consuming()

        except (pika.exceptions.AMQPConnectionError, pika.exceptions.ConnectionClosed):
            if self.retry < self.max_retry:
                self.retry += 1
                self.logger.error("Connection is closed or refused, retrying in %s seconds" % self.retry)
                time.sleep(self.retry)
                return self.consume_message_bus()
            elif self.retry >= self.max_retry:
                self.logger.error("Too many attempts to connect. Exiting now.")
                return
        except:
            self.logger.exception("An unexpected error occurred")

        if self.channel is not None:
            self.channel.stop_consuming()
            self.channel = None

        if self.connection is not None:
            self.connection.close()
            self.connection = None

    def on_bus_message(self, channel, method_frame, header_frame, body):
        """
        Callback that gets called for any event on the Carbon Black event bus
        """

        try:
            # there are two messages that get broadcast that we really
            # don"t care about.  They have to do with feed synchronization
            # and other internal book-keeping
            if method_frame.routing_key in self.capture_events:
                event = {
                    "content_type": header_frame.content_type,
                    "routing_key": method_frame.routing_key,
                    "body": body
                }
                self.logger.debug("Received Message: %s - %s" % (header_frame.content_type, method_frame.routing_key))
                self.processor_pool.apply_async(process_event, (self.event_processor, event))

            else:
                self.logger.debug("Uknown message info: %s" % method_frame.routing_key)

        except:
            self.logger.exception("Error processing bus message")

        finally:
            # need to make sure we ack the messages so they don"t get left un-acked in the queue
            # we set multiple to true to ensure that we ack all previous messages
            channel.basic_ack(delivery_tag=method_frame.delivery_tag, multiple=True)

    def get_bus_credentials(self):
        username = self.forwarder_options.get("rabbit_mq_username", "")
        password = self.forwarder_options.get("rabbit_mq_password", "")
        if not username or not password:
            self.logger.info("Retrieving Rabbit MQ credentials from cb.conf")
            for line in open('/etc/cb/cb.conf').readlines():
                if line.strip().startswith('RabbitMQUser'):
                    username = line.split('=')[1].strip()
                if line.strip().startswith('RabbitMQPassword'):
                    password = line.split('=')[1].strip()
        return username, password

    def set_capture_events_from_config(self):
        """
        Retrieve which events to capture from the config
        """

        event_config = [
            {
                "config_key": "events_watchlist",
                "events": [
                    "watchlist.hit.process",
                    "watchlist.hit.binary",
                    "watchlist.storage.hit.process",
                    "watchlist.storage.hit.binary"
                ],
                "options": self.forwarder_options.get("wlhitnotifenabled", "0")
            },
            {
                "config_key": "events_feed",
                "events": [
                    "feed.ingress.hit.process",
                    "feed.ingress.hit.binary",
                    "feed.ingress.hit.host",
                    "feed.storage.hit.process",
                    "feed.storage.hit.binary",
                    "feed.query.hit.process",
                    "feed.query.hit.binary"
                ],
                "options": self.forwarder_options.get("feedhitnotif", "0")
            },
            {
                "config_key": "events_alert",
                "events": [
                    "alert.watchlist.hit.ingress.process",
                    "alert.watchlist.hit.ingress.binary",
                    "alert.watchlist.hit.ingress.host",
                    "alert.watchlist.hit.query.process",
                    "alert.watchlist.hit.query.binary"
                ],
                "options": self.forwarder_options.get("alertnotifenabled", "0")
            },
            {
                "config_key": "events_raw_sensor",
                "events": [
                    "ingress.event.process",
                    "ingress.event.procstart",
                    "ingress.event.netconn",
                    "ingress.event.procend",
                    "ingress.event.childproc",
                    "ingress.event.moduleload",
                    "ingress.event.module",
                    "ingress.event.filemod",
                    "ingress.event.regmod"
                ],
                "options": self.forwarder_options.get("rawsensnotifenabled", "0")
            },
            {
                "config_key": "events_binary_observed",
                "events": ["binarystore.file.added"],
                "options": self.forwarder_options.get("binobsnotifenabled", "0")
            },
            {
                "config_key": "events_binary_upload",
                "events": ["binary.host.observed"],
                "options": self.forwarder_options.get("binuplnotifenabled", "0")
            }
        ]

        self.capture_events = []
        for event_type in event_config:
            events = self.forwarder_options.get(event_type["config_key"], "0").lower()
            if events == "all":
                self.capture_events.extend(event_type["events"])
            elif events != "0":
                events_from_config = events.split(",")
                events_to_capture = list(set(events_from_config) & set(event_type["events"]))
                self.capture_events.extend(events_to_capture)

        self.logger.info("Configured to capture events: %s" % self.capture_events)
