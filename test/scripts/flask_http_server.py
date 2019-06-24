#!/usr/bin/env python

from flask import Flask, request, Response
import gzip
import sys


def get_flask_server():
    flask_server = Flask("event-forwarder-test")

    @flask_server.route("/api/submit", methods=["POST"])
    def submit_file():
        if request.headers.get("Content-Encoding", "") == "gzip":
            print("gzipped data:")
            print("{}".format(gzip.decompress(request.data)))
        else:
            print("raw data:")
            print("{}".format(request.json))
        return Response("OK", status=200, mimetype="text/plain")

    @flask_server.route("/")
    def index():
        return Response("OK", status=200, mimetype="text/plain")

    return flask_server


def main():
    port = 8000

    flask_server = get_flask_server()
    flask_server.run("0.0.0.0", port, debug=True)


if __name__ == '__main__':
    sys.exit(main())
