#!/usr/bin/env python

import argparse
import json
import os
import sys

from cbeventforwarder.lib import event_parser


def walk_dir(indir, outdir):
    # initialize EventParser
    parser = event_parser.EventParser(options={"server_name": "cbserver"})

    for format in ["json", "protobuf"]:
        if not os.path.isdir(os.path.join(indir, format)):
            continue

        format_path = os.path.join(indir, format)
        for routing_key in [x for x in os.listdir(format_path) if os.path.isdir(os.path.join(format_path, x))]:
            print "Processing %s/%s..." % (format, routing_key)
            out_path = os.path.join(outdir, format, routing_key)
            os.makedirs(out_path, 0755)

            rk_path = os.path.join(format_path, routing_key)

            for fn in [x for x in os.listdir(rk_path) if os.path.isfile(os.path.join(rk_path, x))]:
                output = None
                try:
                    event_data = open(os.path.join(rk_path, fn), 'rb').read()
                except Exception as e:
                    import traceback
                    sys.stderr.write("Exception reading file %s\n" % os.path.join(rk_path, fn))
                    sys.stderr.write(traceback.format_exc())
                    sys.stderr.flush()
                    continue

                try:
                    if format == "json":
                        output = parser.parse_event_json(event_data, routing_key)
                    elif format == "protobuf":
                        output = parser.parse_event_pb(event_data, routing_key)
                except Exception as e:
                    import traceback
                    sys.stderr.write("Exception processing file %s\n" % os.path.join(rk_path, fn))
                    sys.stderr.write(traceback.format_exc())
                    sys.stderr.flush()
                else:
                    with open(os.path.join(out_path, fn), 'wb') as fp:
                        for l in output:
                            fp.write(json.dumps(l) + "\n")


def main():
    parser = argparse.ArgumentParser("Run raw events through existing cb-event-forwarder parser")
    parser.add_argument("indir", help="Input directory containing files to process")
    parser.add_argument("outdir", help="Output directory for json files")
    opts = parser.parse_args()

    return walk_dir(opts.indir, opts.outdir)

if __name__ == '__main__':
    sys.exit(main())
