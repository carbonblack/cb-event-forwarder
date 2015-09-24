import json
import logging

import event_helpers

LOGGER = logging.getLogger(__name__)


class EventParser(object):
    def __init__(self, options):
        self.sensor_id_to_details_map = {}

        self.cb_server = options.get("server_name", None)

    def parse_event_pb(self, protobuf_bytes, routing_key):
        """
        Parse a Carbon Black event bus message that is in a protobuf format
        """

        (sensor_id, event_obj) = event_helpers.protobuf_to_obj_and_host(protobuf_bytes)

        # since we have multiple object types
        # we overwrite some fields in the protobuf based
        # event object
        event_obj["event_type"] = event_obj["type"]
        event_obj["type"] = routing_key

        # and the server identifier
        if self.cb_server is not None:
            event_obj["cb_server"] = self.cb_server

        return [event_obj]

    @staticmethod
    def get_process_guid_from_id(unique_id):
        """
        Extract the process ID from a process' unique ID.
        A process' unique ID may contain a segment ID, which need to be stripped out (e.g. "<process_id>-<segment_id>")
        """

        parts = unique_id.split("-")
        if len(parts) == 6:
            parts = parts[0:5]
            return "-".join(parts)
        else:
            return unique_id

    def fix_process_guids(self, event):
        """
        Extract and map process GUIDs based on their unique IDs
        """

        if "docs" in event:
            for d in event["docs"]:
                if "unique_id" in d:
                    d["process_guid"] = self.get_process_guid_from_id(d["unique_id"])
                if "parent_unique_id" in d:
                    d["parent_guid"] = self.get_process_guid_from_id(d["parent_unique_id"])

        if "process_id" in event:
            pid = event["process_id"]
            event["process_guid"] = pid
            del event["process_id"]

        return event

    def parse_event_json(self, msg_body, routing_key):
        """
        Parse a Carbon Black event bus message that is in a JSON format
        """

        json_obj = json.loads(msg_body)
        json_obj["type"] = routing_key

        ret_events = []

        # for two types of alerts - the matches
        # are coalesed into a single alert
        # for our cases where we split them apart
        if routing_key.startswith("watchlist.hit."):
            for d in json_obj["docs"]:
                c = json_obj.copy()
                c["docs"] = [d]
                ret_events.append(c)

        else:
            ret_events.append(json_obj)

        for event_obj in ret_events:
            if "highlights" in event_obj:
                del event_obj["highlights"]

            # keep the timestamp field name consistently
            if "event_timestamp" in event_obj:
                event_obj["timestamp"] = event_obj["event_timestamp"]
                del event_obj["event_timestamp"]

            #
            # when it makes sense add sensor
            # information to the object.  This is dependent
            # on the object type
            #
            if routing_key == "watchlist.storage.hit.process" or routing_key == "watchlist.hit.process":
                d = event_obj["docs"]
                if "hostname" in d:
                    d["computer_name"] = d["hostname"]

            else:
                # rather than track the correct objects - just look
                # for a sensor id
                if "hostname" in event_obj:
                    event_obj["computer_name"] = event_obj["hostname"]

            # fix up terminology on process id/guid so that "process_guid" always
            # refers to the process guid (minus segment)
            event_obj = self.fix_process_guids(event_obj)

            # add the "cb_server" field to the json.  This is used
            # to tie the event to a specific cluster/server in environments
            # where multiple servers are deployed
            # some of the watchlist events have a server_name field but that
            # might reference a minion within a cluster or can sometimes be blank
            if self.cb_server is not None:
                event_obj["cb_server"] = self.cb_server

        return ret_events

    def parse_events(self, content_type, routing_key, body):
        """
        Parse a Rabbit MQ event based on it's contest type and routing key
        Note: Result is an array of events as one message bus event may yield multiple events
        """

        try:
            if "application/protobuf" == content_type:
                # if the type is protobuf - we handle it here
                # this means it is a raw sensor event
                return self.parse_event_pb(body, routing_key)

            elif "application/json" == content_type:
                # handle things already in JSON
                return self.parse_event_json(body, routing_key)

            else:
                raise ValueError("Unexpected content_type: %s" % content_type)

        except Exception as e:
            LOGGER.exception("%s" % e)

            return []

