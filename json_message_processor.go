package main

import (
	"encoding/json"
	"github.com/carbonblack/cb-event-forwarder/deepcopy"
	"reflect"
	"strconv"
	"strings"
)

func stripSegmentId(v string) string {
	return v[:36]
}

func fixupMessage(msg map[string]interface{}) {
	// go through each key and fix up as necessary
	for key, value := range msg {
		switch key {
		case "highlights":
			delete(msg, "highlights")
		case "event_timestamp":
			msg["timestamp"] = value
			delete(msg, "event_timestamp")
		case "hostname":
			msg["computer_name"] = value
		case "process_id":
			msg["process_guid"] = value
		case "unique_id":
			if uniqueId, ok := value.(string); ok {
				msg["process_guid"] = stripSegmentId(uniqueId)
			}
		case "parent_unique_id":
			if uniqueId, ok := value.(string); ok {
				msg["parent_guid"] = stripSegmentId(uniqueId)
			}
		case "md5":
		case "parent_md5":
		case "process_md5":
			if md5, ok := value.(string); ok {
				if len(md5) == 32 {
					msg[key] = strings.ToUpper(md5)
				}
			}
		case "ioc_type":
			// if the ioc_type is a map and it contains a key of "md5", uppercase it
			v := reflect.ValueOf(value)
			if v.Kind() == reflect.Map && v.Type().Key().Kind() == reflect.String {
				ioc_type := value.(map[string]interface{})
				if md5value, ok := ioc_type["md5"]; ok {
					if md5, ok := md5value.(string); ok {
						if len(md5) == 32 {
							ioc_type["md5"] = strings.ToUpper(md5)
						}
					}
				}
			}
		case "comms_ip":
		case "interface_ip":
			if value, ok := value.(json.Number); ok {
				ipaddr, err := strconv.ParseInt(value.String(), 10, 32)
				if err != nil {
					msg[key] = GetIPv4AddressSigned(int32(ipaddr))
				}
			}
		}
	}
}

func ProcessJSONMessage(msg map[string]interface{}, routingKey string) ([]map[string]interface{}, error) {
	msg["type"] = routingKey
	fixupMessage(msg)

	msgs := make([]map[string]interface{}, 0, 1)

	// explode watchlist hit messages
	if strings.HasPrefix(routingKey, "watchlist.hit.") || strings.HasPrefix(routingKey, "watchlist.storage.hit.") {
		if val, ok := msg["docs"]; ok {
			subdocs := deepcopy.Iface(val).([]interface{})
			delete(msg, "docs")

			for _, submsg := range subdocs {
				submsg := submsg.(map[string]interface{})
				newMsg := deepcopy.Iface(msg).(map[string]interface{})
				newSlice := make([]map[string]interface{}, 0, 1)
				newDoc := deepcopy.Iface(submsg).(map[string]interface{})
				fixupMessage(newDoc)
				newSlice = append(newSlice, newDoc)
				newMsg["docs"] = newSlice
				msgs = append(msgs, newMsg)
			}
		} else {
			// TODO: error condition: "docs" doesn't exist
		}
	} else {
		msgs = append(msgs, msg)
	}

	// fix up output msgs
	for _, msg := range msgs {
		fixupMessage(msg)
	}

	return msgs, nil
}
