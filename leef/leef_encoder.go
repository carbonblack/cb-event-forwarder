package leef

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strconv"
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

		switch reflect.ValueOf(msg[key]).Type().Kind() {
		case reflect.Uint:
			val = strconv.FormatUint(uint64(msg[key].(uint)), 10)
		case reflect.Uint8:
			val = strconv.FormatUint(uint64(msg[key].(uint8)), 10)
		case reflect.Uint16:
			val = strconv.FormatUint(uint64(msg[key].(uint16)), 10)
		case reflect.Uint32:
			val = strconv.FormatUint(uint64(msg[key].(uint32)), 10)
		case reflect.Uint64:
			val = strconv.FormatUint(msg[key].(uint64), 10)

		case reflect.Int:
			val = strconv.FormatInt(int64(msg[key].(int)), 10)
		case reflect.Int8:
			val = strconv.FormatInt(int64(msg[key].(int8)), 10)
		case reflect.Int16:
			val = strconv.FormatInt(int64(msg[key].(int16)), 10)
		case reflect.Int32:
			val = strconv.FormatInt(int64(msg[key].(int32)), 10)
		case reflect.Int64:
			val = strconv.FormatInt(msg[key].(int64), 10)

		case reflect.Float32:
			val = strconv.FormatFloat(float64(msg[key].(float32)), 'f', -1, 32)
		case reflect.Float64:
			val = strconv.FormatFloat(msg[key].(float64), 'f', -1, 64)

		case reflect.String:
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
			t, err := json.Marshal(msg[key])
			if err != nil {
				// TODO: error condition
				continue
			}
			val = string(t)
		}
		kvPairs = append(kvPairs, fmt.Sprintf("%s=%s", key, val))
	}

	return fmt.Sprintf("%s%s", generateHeader(cbVersion, messageType), strings.Join(kvPairs, "\t")), nil
}
