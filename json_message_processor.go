package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/carbonblack/cb-event-forwarder/deepcopy"
	"reflect"
	"strconv"
	"strings"
)

func parseFullGuid(v string) (string, int, error) {
	var segmentNumber int64
	var err error

	segmentNumber = 1

	switch {
	case len(v) < 36:
		return v, int(segmentNumber), errors.New("Truncated GUID")
	case len(v) == 36:
		return v, int(segmentNumber), nil
	case len(v) == 45:
		segmentNumber, err = strconv.ParseInt(v[37:], 16, 32)
		if err != nil {
			segmentNumber = 1
		}
	default:
		err = errors.New("Truncated GUID")
	}

	return v[:36], int(segmentNumber), err
}

func fixupMessage(msg map[string]interface{}) {
	// go through each key and fix up as necessary
	for key, value := range msg {
		switch {
		case key == "highlights":
			delete(msg, "highlights")
		case key == "event_timestamp":
			msg["timestamp"] = value
			delete(msg, "event_timestamp")
		case key == "hostname":
			msg["computer_name"] = value
		case key == "process_id":
			msg["process_guid"] = value

			if uniqueId, ok := value.(string); ok {
				processGuid, segment, _ := parseFullGuid(uniqueId)

				// TODO: not happy about reaching in to the "config" object for this
				// Only add the link to the process if we haven't already done so. The "unique_id" (below)
				// should always take precedence since it will include a segment number whereas the "process_id"
				// does not.
				_, ok := msg["link_process"]
				if config.CbServerURL != "" && !ok {
					msg["link_process"] = fmt.Sprintf("%s#analyze/%s/%d", config.CbServerURL, processGuid, segment)
				}
			}
		case key == "unique_id":
			if uniqueId, ok := value.(string); ok {
				processGuid, segment, _ := parseFullGuid(uniqueId)
				msg["process_guid"] = processGuid
				// TODO: not happy about reaching in to the "config" object for this
				if config.CbServerURL != "" {
					msg["link_process"] = fmt.Sprintf("%s#analyze/%s/%d", config.CbServerURL, processGuid, segment)
				}
			}
		case key == "parent_unique_id":
			if uniqueId, ok := value.(string); ok {
				processGuid, segment, _ := parseFullGuid(uniqueId)
				msg["parent_guid"] = processGuid
				// TODO: not happy about reaching in to the "config" object for this
				if config.CbServerURL != "" {
					msg["link_parent"] = fmt.Sprintf("%s#analyze/%s/%d", config.CbServerURL, processGuid, segment)
				}
			}
		case key == "md5" || key == "parent_md5" || key == "process_md5":
			if md5, ok := value.(string); ok {
				if len(md5) == 32 {
					msg[key] = strings.ToUpper(md5)
					if config.CbServerURL != "" {
						keyName := "link_" + key
						msg[keyName] = fmt.Sprintf("%s#/binary/%s", config.CbServerURL, msg[key])
					}
				}
			}
		case key == "ioc_type":
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
		case key == "comms_ip" || key == "interface_ip":
			if value, ok := value.(json.Number); ok {
				ipaddr, err := strconv.ParseInt(value.String(), 10, 32)
				if err == nil {
					msg[key] = GetIPv4AddressSigned(int32(ipaddr))
				}
			}
		case key == "sensor_id":
			if value, ok := value.(json.Number); ok {
				hostId, err := strconv.ParseInt(value.String(), 10, 32)
				if err == nil && config.CbServerURL != "" {
					// TODO: not happy about reaching in to the "config" object for this
					msg["link_sensor"] = fmt.Sprintf("%s#/host/%d", config.CbServerURL, hostId)
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

	return msgs, nil
}
