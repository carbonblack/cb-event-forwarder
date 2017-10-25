from six.moves.BaseHTTPServer import BaseHTTPRequestHandler
from six.moves import socketserver
import sys


class RequestHandler(BaseHTTPRequestHandler):
    def do_POST(self):
        print("Got data:")
        print(self.rfile.read())


def main():
    port = 8000
    handler = RequestHandler

    httpd = socketserver.TCPServer(("", port), handler)
    httpd.serve_forever()


if __name__ == '__main__':
    sys.exit(main())

