#!/usr/bin/env python

import json
import sys
import timeit

from cbeventforwarder.lib import event_parser

parser = event_parser.EventParser(options={"server_name": "cbserver"})

def parse_json():
    json_file = "../raw_data/json/watchlist.hit.process/0.json"
    d = open(json_file, 'rb').read()
    json.dumps(parser.parse_event_json(d, "watchlist.hit.process"))

def parse_pb():
    pb_file = "../raw_data/protobuf/ingress.event.process/0.protobuf"
    d = open(pb_file, 'rb').read()
    json.dumps(parser.parse_event_pb(d, "ingress.event.process"))

def main():
    number = 10000
    precision = 4

    stmt="parse_json()"
    best = timeit.timeit(setup="from __main__ import parse_json", stmt=stmt, number=number)
    print "%s:" % stmt,
    usec = best * 1e6 / number
    if usec < 1000:
        print "%.*g usec per loop" % (precision, usec)
    else:
        msec = usec / 1000
        if msec < 1000:
            print "%.*g msec per loop" % (precision, msec)
        else:
            sec = msec / 1000
            print "%.*g sec per loop" % (precision, sec)

    stmt="parse_pb()"
    best = timeit.timeit(setup="from __main__ import parse_pb", stmt=stmt, number=number)
    print "%s:" % stmt,
    usec = best * 1e6 / number
    if usec < 1000:
        print "%.*g usec per loop" % (precision, usec)
    else:
        msec = usec / 1000
        if msec < 1000:
            print "%.*g msec per loop" % (precision, msec)
        else:
            sec = msec / 1000
            print "%.*g sec per loop" % (precision, sec)


if __name__ == '__main__':
    sys.exit(main())
