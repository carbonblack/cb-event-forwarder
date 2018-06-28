package encoder

import (
	"encoding/json"
	"errors"
	"fmt"
	log "github.com/sirupsen/logrus"
	"reflect"
	"sort"
	"strings"
)

type CEFEncoder struct {
	productVendorName string
	productName       string
	productVersion    string
	cefVersion        string
	formatter         *strings.Replacer
	eventSeverity     int
}

func NewCEFEncoder(severity int) CEFEncoder {
	myformatter := strings.NewReplacer(
		"\\", "\\\\",
		"\n", "\\n",
		"\r", "\\r",
		"\t", "\\t",
		"=", "\\=",
	)

	temp := CEFEncoder{productVendorName: "CB", productName: "CB", productVersion: "5.1", cefVersion: "1.0", formatter: myformatter, eventSeverity: severity}
	return temp
}

func (c *CEFEncoder) generateHeader(cbVersion, eventType string) string {
	// name | Severity | Extension
	return fmt.Sprintf("CEF:%s|%s|%s|%s|%s|%s|%d|", c.cefVersion, c.productVendorName, c.productName, cbVersion,
		eventType, eventType, c.eventSeverity)
}

func (c *CEFEncoder) normalizeAddToMap(msg map[string]interface{}, temp map[string]interface{}) {
	outboundConnections := map[string]string{
		"local_ip":    "src",
		"remote_ip":   "dst",
		"protocol":    "proto",
		"local_port":  "srcPort",
		"remote_port": "dstPort",
	}
	inboundConnections := map[string]string{
		"local_ip":    "dst",
		"remote_ip":   "src",
		"protocol":    "proto",
		"local_port":  "dstPort",
		"remote_port": "srcPort",
	}

	leefMap := outboundConnections
	if directionality, ok := temp["direction"]; ok {
		if directionality == "inbound" {
			leefMap = inboundConnections
		}
	}

	for key, value := range temp {
		if newKey, ok := leefMap[key]; ok {
			msg[newKey] = value
		}
	}
}

func (e *CEFEncoder) String() string {
	return "cef"
}

func (c *CEFEncoder) Encode(msg map[string]interface{}) (string, error) {

	keyNames := make([]string, 0)
	kvPairs := make([]string, 0)

	add_to_kvs := func(key string, val interface{}) {
		log.Debugf("adding key = val to kvPairs %s=%s", key, val)

		kvPairs = append(kvPairs, fmt.Sprintf("%s=%s", key, val))
	}

	normalize_add_to_kvs := func(msg map[string]interface{}, temp map[string]interface{}) {
		outboundConnections := map[string]string{
			"local_ip":    "src",
			"remote_ip":   "dst",
			"protocol":    "proto",
			"local_port":  "srcPort",
			"remote_port": "dstPort",
		}
		inboundConnections := map[string]string{
			"local_ip":    "dst",
			"remote_ip":   "src",
			"protocol":    "proto",
			"local_port":  "dstPort",
			"remote_port": "srcPort",
		}

		leefMap := outboundConnections
		if directionality, ok := temp["direction"]; ok {
			if directionality == "inbound" {
				leefMap = inboundConnections
			}
		}

		for key, value := range temp {
			if newKey, ok := leefMap[key]; ok {
				//msg[newKey] = value
				add_to_kvs(newKey, value)
			}
		}
	}

	// promote "docs" up to the root
	if val, ok := msg["docs"]; ok {
		// At some point, I had to cast to an interface{} before casting to a map[string]interface{}.
		// Now it seems that I can cast directly to the map. Leaving this in just in case...
		if subdocs, ok := val.([]interface{}); ok {
			if len(subdocs) != 1 {
				return "", errors.New("More than one entry in docs[]")
			}
			if kv, ok := subdocs[0].(map[string]interface{}); ok {
				for key, value := range kv {
					//msg[key] = value
					add_to_kvs(key, value)
				}
				//delete(msg, "docs")
			} else {
				// TODO: remove before flight
				return "", errors.New("could not map docs[0] to map[string]interface{}")
			}
		} else if subdocs, ok := val.([]map[string]interface{}); ok {
			if len(subdocs) != 1 {
				return "", errors.New("More than one entry in docs[]")
			}
			for key, value := range subdocs[0] {
				//msg[key] = value
				add_to_kvs(key, value)
			}
			//delete(msg, "docs")
		} else {
			// TODO: remove before flight
			return "", errors.New("could not map docs to []interface{}")
		}
	}

	//
	// https://github.com/carbonblack/cb-event-forwarder/issues/40
	//
	// It was requested to add a new key pair into the LEEF output at the top level
	// for the specified ioc_attr's
	//
	// local_ip -> src
	// local_port -> srcPort
	// protocol -> proto
	// remote_ip -> dst
	// remote_port ->dstPort
	//

	if ioc_attr, ok := msg["ioc_attr"]; ok {
		val := reflect.ValueOf(ioc_attr)

		//
		// We weren't sure if we had seen ioc_attr as a string before.
		// We are just handling this case.  Decode as json and add appropriate fields to top level
		//

		if val.Kind() == reflect.String {
			var temp map[string]interface{}
			decoder := json.NewDecoder(strings.NewReader(ioc_attr.(string)))

			// Ensure that we decode numbers in the JSON as integers and *not* float64s
			decoder.UseNumber()

			if err := decoder.Decode(&temp); err != nil {
				return "", errors.New("Received error when unmarshaling JSON ioc_attr")
			}
			//
			// Add fields from temp into msg using QRadar normalized IP fields
			//
			normalize_add_to_kvs(msg, temp)

		} else if val.Kind() == reflect.Map {
			//
			// This is the expected case.  Map appropriate fields to the top level
			//
			if kv, ok := ioc_attr.(map[string]interface{}); ok {
				//
				// Add fields from kv into msg using QRadar normalized IP fields
				//
				normalize_add_to_kvs(msg, kv)
			}
		}
	}

	//
	// For netconns we want to map remote and local ports to QRadar normalized fields
	//

	if msg["type"] == "ingress.event.netconn" {
		normalize_add_to_kvs(msg, msg)
	}

	for key, _ := range msg {
		if key != "docs" && key != "ioc_attr" {
			keyNames = append(keyNames, key)
		}
	}

	// message type applied to messages without an explicit message type.
	// the code below will promote the "type" to messageType in the LEEF header.
	messageType := "unknown.event.type"
	cbVersion := c.productVersion

	sort.Strings(keyNames)
	for _, key := range keyNames {
		if !reflect.ValueOf(msg[key]).IsValid() {
			continue
		}
		var ret_val string = ""
		cbVersion, messageType, ret_val = msgfunc(cbVersion, messageType, msg, key)
		log.Debugf("adding key = val to kvPairs %s=%s", key, ret_val)

		kvPairs = append(kvPairs, fmt.Sprintf("%s=%s", key, ret_val))
	}

	// override "procstart" with "process" as this is what the LEEF decoder in QRadar is expecting
	if messageType == "ingress.event.procstart" {
		messageType = "ingress.event.process"
	}

	log.Debugf("kvPairs = %s", kvPairs)

	joined_kv := strings.Join(kvPairs, " ")

	ret := fmt.Sprintf("%s%s", c.generateHeader(cbVersion, messageType), joined_kv)

	return ret, nil
}
