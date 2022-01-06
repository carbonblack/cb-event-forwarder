#!/usr/bin/python3.8

import socketserver
from http.server import SimpleHTTPRequestHandler
import sys
import enum


class ServerTypes(enum.Enum):
    TCP = 1
    UDP = 2
    HTTP = 3


class OutputFileMetaclass(type):
    def __new__(cls, clsname, bases, attrs, output_file="/tmp/output"):
        attrs["output_file"] = output_file
        return_class = super(OutputFileMetaclass, cls).__new__(cls, clsname, bases, attrs)
        return return_class


class SocketHandler(socketserver.StreamRequestHandler):

    def handle(self):
        content = self.rfile.read()
        with open(self.output_file, "a+") as output_file:
            output_file.write(content.decode("utf-8"))


class HTTPHandler(SimpleHTTPRequestHandler):

    def do_POST(self):
        content = self.rfile.read()
        with open(self.output_file, "a+") as output_file:
            output_file.write(content.decode("utf-8"))
        self.send_response(200)
        self.end_headers()


def handler_maker(output_file="/tmp/output", base_handler=SocketHandler):
    return type("DYNHandler", (base_handler,), {"metaclass": OutputFileMetaclass, "output_file": output_file})


def serve(host, port, output_file, server_type=ServerTypes.TCP):
    type_to_server_class = {ServerTypes.TCP: socketserver.TCPServer, ServerTypes.UDP: socketserver.UDPServer,
                            ServerTypes.HTTP: socketserver.TCPServer}
    server_class = type_to_server_class[server_type]
    base_handler = HTTPHandler if server_type == ServerTypes.HTTP else SocketHandler
    with server_class((host, port), handler_maker(output_file, base_handler)) as server:
        server.serve_forever()


if __name__ == "__main__":
    (_, host, port, file, mode) = sys.argv
    integer_port = int(port)
    server_type_enum = ServerTypes[mode.upper()]
    serve(host, integer_port, file, server_type_enum)