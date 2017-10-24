	package leef

import (
	"encoding/json"
	"errors"
	"fmt"
	log "github.com/sirupsen/logrus"
	"reflect"
	"sort"
	"strings"
)

var (
	productVendorName string
	productName       string
	productVersion    string
	leefVersion       string
	formatter         *strings.Replacer
)

var jsonNumberType reflect.Type

func init() {
	productVendorName = "CB"
	productName = "CB"
	productVersion = "5.1"
	leefVersion = "1.0"

	formatter = strings.NewReplacer(
		"\\", "\\\\",
		"\n", "\\n",
		"\r", "\\r",
		"\t", "\\t",
		"=", "\\=",
	)

	var t json.Number
	jsonNumberType = reflect.ValueOf(t).Type()
}

func generateHeader(cbVersion, eventType string) string {
	return fmt.Sprintf("LEEF:%s|%s|%s|%s|%s|", leefVersion, productVendorName, productName, cbVersion,
		eventType)
}

func normalizeAddToMap(msg map[string]interface{}, temp map[string]interface{}) {
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

func Encode(msg map[string]interface{}) (string, error) {
	keyNames := make([]string, 0)
	kvPairs := make([]string, 0)

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
					msg[key] = value
				}
				delete(msg, "docs")
			} else {
				// TODO: remove before flight
				return "", errors.New("could not map docs[0] to map[string]interface{}")
			}
		} else if subdocs, ok := val.([]map[string]interface{}); ok {
			if len(subdocs) != 1 {
				return "", errors.New("More than one entry in docs[]")
			}
			for key, value := range subdocs[0] {
				msg[key] = value
			}
			delete(msg, "docs")
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
			normalizeAddToMap(msg, temp)

		} else if val.Kind() == reflect.Map {
			//
			// This is the expected case.  Map appropriate fields to the top level
			//
			if kv, ok := ioc_attr.(map[string]interface{}); ok {
				//
				// Add fields from kv into msg using QRadar normalized IP fields
				//
				normalizeAddToMap(msg, kv)
			}
		}
	}

	//
	// For netconns we want to map remote and local ports to QRadar normalized fields
	//

	if msg["type"] == "ingress.event.netconn" {
		normalizeAddToMap(msg, msg)
	}

	for key, _ := range msg {
		keyNames = append(keyNames, key)
	}

	// message type applied to messages without an explicit message type.
	// the code below will promote the "type" to messageType in the LEEF header.
	messageType := "unknown.event.type"
	cbVersion := productVersion

	sort.Strings(keyNames)
	for _, key := range keyNames {

		var val string

		// remove all invalid values from the output
		if !reflect.ValueOf(msg[key]).IsValid() {
			continue
		}

		switch reflect.ValueOf(msg[key]).Type().Kind() {

		case reflect.Map:
		case reflect.Array:
		case reflect.Slice:
			// if the value is a map, array or slice, then format as JSON
			t, err := json.Marshal(msg[key])
			if err != nil {
				log.Infof("Could not marshal key %s with value %v into JSON: %s, skipping", key, msg[key], err.Error())
				continue
			}
			val = string(t)

		case reflect.String:
			// make sure to format strings with the appropriate character escaping
			// also make sure we reflect the "type" and "cb_version" on to the message header, if present
			if reflect.ValueOf(msg[key]).Type() == jsonNumberType {
				val = msg[key].(json.Number).String()
			} else {
				val = msg[key].(string)
				if key == "type" {
					messageType = val
				} else if key == "cb_version" {
					cbVersion = val
				}
			}
			val = formatter.Replace(val)

		default:
			// simplify and use fmt.Sprintf to format the output
			val = fmt.Sprintf("%v", msg[key])
		}

		log.Infof("adding key = val to kvPairs %s=%s",key,val)

		kvPairs = append(kvPairs, fmt.Sprintf("%s=%s", key, val))
	}

	// override "procstart" with "process" as this is what the LEEF decoder in QRadar is expecting
	if messageType == "ingress.event.procstart" {
		messageType = "ingress.event.process"
	}

	log.Infof("kvPairs = %v",kvPairs)

	joined_kv := strings.Join(kvPairs,"\t")
	log.Infof("joined kvparis = %v",joined_kv)
	log.Infof("joined kvpairs = %s",joined_kv)

	ret  := fmt.Sprintf("%s%v", generateHeader(cbVersion, messageType), joined_kv)

	log.Infof("Returnining (from encode) : %v", ret)

	return ret,nil
}
