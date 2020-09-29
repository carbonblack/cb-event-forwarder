#!/usr/bin/env python

import os
import sys
import optparse
import pika
import threading
import time
import pprint

g_stats = {}
g_stats_lock = threading.RLock()


def get_mq_user_from_cbconf():
    for line in open('/etc/cb/cb.conf').readlines():
        if line.strip().startswith('RabbitMQUser'):
            return line.split('=')[1].strip()


def get_mq_pass_from_cbconf():
    for line in open('/etc/cb/cb.conf').readlines():
        if line.strip().startswith('RabbitMQPassword'):
            return line.split('=')[1].strip()


def on_bus_msg(channel, method_frame, header_frame, body):
    with g_stats_lock:
        if method_frame.routing_key not in g_stats:
            g_stats[method_frame.routing_key] = 1
        else:
            g_stats[method_frame.routing_key] += 1


def on_bus_raw_msg(channel, method_frame, header_frame, body):
    with g_stats_lock:
        if "api.rawsensordata" not in g_stats:
            g_stats["api.rawsensordata"] = 1
        else:
            g_stats["api.rawsensordata"] += 1


def bus_event_loop(cb_hostname, rabbit_mq_user, rabbit_mq_pass):
    credentials = pika.PlainCredentials(rabbit_mq_user, rabbit_mq_pass)
    parameters = pika.ConnectionParameters(cb_hostname,
                                           5004,
                                           '/',
                                           credentials)

    connection = pika.BlockingConnection(parameters)
    channel = connection.channel()

    queue_name = 'api_event_exporter_pid_%d' % os.getpid()

    # make sure you use auto_delete so the queue isn't left filling
    # with events when this program exists.
    channel.queue_declare(queue=queue_name, auto_delete=True)

    channel.queue_bind(exchange='api.events', queue=queue_name, routing_key='#')

    channel.basic_consume(on_bus_msg, queue=queue_name, no_ack=True)

    print("[+]: Subscribed to api.events")

    t1 = threading.Thread(target=channel.start_consuming)
    t1.daemon = True
    t1.start()

def bus_raw_event_loop(cb_hostname, rabbit_mq_user, rabbit_mq_pass):
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


    channel.basic_consume(on_bus_raw_msg, queue=queue_name, no_ack=True)

    print("[+]: Subscribed to api.rawsensordata")

    t1 = threading.Thread(target=channel.start_consuming)
    t1.daemon = True
    t1.start()


def build_cli_parser():
    parser = optparse.OptionParser(usage="%prog [options]",
                                   description="Process VMware Carbon Black EDR Sensor Event Logs")

    parser.add_option("-u", "--user",
                      action="store",
                      default="cb",
                      dest="user",
                      help="The username for the rabbitMQ pub/sub event bus (default is to pull it from config)")

    parser.add_option("-p", "--pass",
                      action="store",
                      default=None,
                      dest="pwd",
                      help="The password for the rabbitMQ pub/sub event bus (default is to pull it from config)")

    parser.add_option("-c", "--connect",
                      action="store",
                      default="localhost",
                      dest="connect",
                      help="hostname of the EDR server")
    return parser


def output_counter():
    while(True):
        if g_stats:
            pprint.pprint(g_stats)
        time.sleep(3)


if __name__ == '__main__':
    parser = build_cli_parser()
    opts, args = parser.parse_args(sys.argv)

    hostname = opts.connect
    user = opts.user
    pwd = opts.pwd

    if not user:
        user = get_mq_user_from_cbconf()

    if not pwd:
        pwd = get_mq_pass_from_cbconf()

    t1 = threading.Thread(target=output_counter)
    t1.daemon = True
    t1.start()

    bus_raw_event_loop(hostname, user, pwd)
    bus_event_loop(hostname, user, pwd)

    while True:
        time.sleep(1)
