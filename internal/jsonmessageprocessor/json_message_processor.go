package jsonmessageprocessor

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/carbonblack/cb-event-forwarder/internal/cbapi"
	"github.com/carbonblack/cb-event-forwarder/internal/deepcopy"
	"github.com/carbonblack/cb-event-forwarder/internal/util"
	log "github.com/sirupsen/logrus"
	"net/url"
	"reflect"
	"regexp"
	"strconv"
	"strings"
)

type JsonMessageProcessor struct {
	DebugFlag   bool
	DebugStore  string
	CbServerURL string
	EventMap    map[string]interface{}
	CbAPI       *cbapi.CbAPIHandler
}

var feedParserRegex = regexp.MustCompile(`^feed\.(\d+)\.(.*)$`)

func parseFullGUID(v string) (string, uint64, error) {

	var segmentNumber uint64
	var err error

	segmentNumber = 1

	switch {
	case len(v) < 36:
		return v, segmentNumber, errors.New("Truncated GUID")
	case len(v) == 36:
		return v, segmentNumber, nil
	case len(v) == 45:
		segmentNumber, err = strconv.ParseUint(v[37:], 16, 64)
		if err != nil {
			segmentNumber = 1
		}
	case len(v) == 49: // Cb Response versions 6.x and above
		segmentNumber, err = strconv.ParseUint(v[37:], 16, 64)
	default:
		err = errors.New("Truncated GUID")
	}

	return v[:36], segmentNumber, err
}

func parseQueryString(encodedQuery map[string]string) (queryIndex string, parsedQuery string, err error) {
	err = nil

	queryIndex, ok := encodedQuery["index_type"]
	if !ok {
		err = errors.New("no index_type included in query")
		return
	}

	rawQuery, ok := encodedQuery["search_query"]
	if !ok {
		err = errors.New("no search_query included in query")
		return
	}

	query, err := url.ParseQuery(rawQuery)
	if err != nil {
		return
	}

	queryArray, ok := query["q"]
	if !ok {
		err = errors.New("no 'q' query parameter provided")
		return
	}

	parsedQuery = queryArray[0]
	return
}
func handleKeyValues(msg map[string]interface{}) {

	var alliance_data_map map[string]map[string]interface{} = make(map[string]map[string]interface{}, 0)
	for key, value := range msg {
		switch {
		case strings.Contains(key, "alliance_"):
			alliance_data := strings.Split(key, "_")
			alliance_data_source := alliance_data[2]
			alliance_data_key := alliance_data[1]
			alliance_map, alreadyexists := alliance_data_map[alliance_data_source]
			if alreadyexists {
				alliance_map[alliance_data_key] = value
			} else {
				temp := make(map[string]interface{})
				temp[alliance_data_key] = value
				alliance_data_map[alliance_data_source] = temp
			}
			delete(msg, key)
		case key == "endpoint":
			endpointstr := ""
			switch value.(type) {
			case string:
				endpointstr = value.(string)
			case []interface{}:
				endpointstr = value.([]interface{})[0].(string)
			}
			parts := strings.Split(endpointstr, "|")
			hostname := parts[0]
			nodeID := parts[1]
			msg["hostname"] = hostname
			msg["node_id"] = nodeID
			delete(msg, "endpoint")
		case key == "highlights_by_doc":
			delete(msg, "highlights_by_doc")
		case key == "highlights":
			delete(msg, "highlights")
		/*case key == "event_timestamp":
		msg["timestamp"] = value
		delete(msg, "event_timestamp")*/
		case key == "timestamp":
			msg["event_timestamp"] = value
			delete(msg, "timestamp")
		case key == "computer_name":
			msg["hostname"] = value
			delete(msg, "computer_name")
		case key == "md5" || key == "parent_md5" || key == "process_md5":
			if md5, ok := value.(string); ok {
				if len(md5) == 32 {
					msg[key] = strings.ToUpper(md5)
				}
			}
		case key == "ioc_type":
			// if the ioc_type is a map and it contains a key of "md5", uppercase it
			v := reflect.ValueOf(value)
			if v.Kind() == reflect.Map && v.Type().Key().Kind() == reflect.String {
				iocType := value.(map[string]interface{})
				if md5value, ok := iocType["md5"]; ok {
					if md5, ok := md5value.(string); ok {
						if len(md5) != 32 && len(md5) != 0 {
							log.WithFields(log.Fields{"MD5 Length": len(md5)}).Warn("MD5 Length was not valid")
						}
						iocType["md5"] = strings.ToUpper(md5)
					}
				}
			} else {
				if iocType, ok := value.(string); ok {
					if iocType == "query" {
						// decode the IOC query
						if rawIocValue, ok := msg["ioc_value"].(string); ok {
							var iocValue map[string]string
							if json.Unmarshal([]byte(rawIocValue), &iocValue) == nil {
								if queryIndex, rawQuery, err := parseQueryString(iocValue); err == nil {
									msg["ioc_query_index"] = queryIndex
									msg["ioc_query_string"] = rawQuery
								}
							}
						}
					}
				}
			}
		case key == "comms_ip" || key == "interface_ip":
			if value, ok := value.(json.Number); ok {
				ipaddr, err := strconv.ParseInt(value.String(), 10, 32)
				if err == nil {
					msg[key] = util.GetIPv4AddressSigned(int32(ipaddr))
				}
			}
		}
	}
	if len(alliance_data_map) > 0 {
		msg["alliance_data"] = alliance_data_map
	}
}
func (jsp *JsonMessageProcessor) fixupMessage(messageType string, msg map[string]interface{}) {
	// go through each key and fix up as necessary

	handleKeyValues(msg)

	hasprocessGUID := false

	// figure out the canonical process guid associated with this message
	if !strings.HasPrefix(messageType, "alert.") {
		if value, ok := msg["unique_id"]; ok {
			if uniqueID, ok := value.(string); ok {
				processGUID, segment, err := parseFullGUID(uniqueID)
				if err == nil {
					msg["process_guid"] = processGUID
					msg["segment_id"] = fmt.Sprintf("%v", segment)
					hasprocessGUID = true
				}
				delete(msg, "unique_id")
			}
		}
	}

	// fall back to process_id in the message
	if !hasprocessGUID {
		if value, ok := msg["process_id"]; ok {
			if uniqueID, ok := value.(string); ok {
				processGUID, segment, _ := parseFullGUID(uniqueID)
				msg["process_guid"] = processGUID
				msg["segment_id"] = fmt.Sprintf("%v", segment)
				hasprocessGUID = true
			}
			delete(msg, "process_id")
		}
	}

	// also deal with parent links
	if value, ok := msg["parent_unique_id"]; ok {
		if uniqueID, ok := value.(string); ok {
			processGUID, segment, _ := parseFullGUID(uniqueID)
			msg["parent_guid"] = processGUID
			msg["parent_segment_id"] = fmt.Sprintf("%v", segment)
		}
		delete(msg, "parent_unique_id")
	}

	// add deep links back into the Cb web UI if configured
	if jsp.CbServerURL != "" {
		AddLinksToMessage(messageType, jsp.CbServerURL, msg)
	}
}

func AddLinksToMessage(messageType, serverURL string, msg map[string]interface{}) {
	// add sensor links when applicable
	if value, ok := msg["sensor_id"]; ok {
		if value, ok := value.(json.Number); ok {
			hostID, err := strconv.ParseInt(value.String(), 10, 32)
			if err == nil {
				msg["link_sensor"] = fmt.Sprintf("%s#/host/%d", serverURL, hostID)
			}
		}
	}

	// add binary links when applicable
	for _, key := range [...]string{"md5", "parent_md5", "process_md5"} {
		if value, ok := msg[key]; ok {
			if md5, ok := value.(string); ok {
				if len(md5) == 32 {
					keyName := "link_" + key
					msg[keyName] = fmt.Sprintf("%s#/binary/%s", serverURL, msg[key])
				}
			}
		}
	}

	// add process links
	if processGUID, ok := msg["process_guid"]; ok {
		if segmentID, ok := msg["segment_id"]; ok {
			msg["link_process"] = fmt.Sprintf("%s#analyze/%v/%v", serverURL, processGUID, segmentID)
		} else {
			msg["link_process"] = fmt.Sprintf("%s#analyze/%v/%v", serverURL, processGUID, 1)
		}
	}

	if parentGUID, ok := msg["parent_guid"]; ok {
		if segmentID, ok := msg["parent_segment_id"]; ok {
			msg["link_parent"] = fmt.Sprintf("%s#analyze/%v/%v", serverURL, parentGUID, segmentID)
		} else {
			msg["link_parent"] = fmt.Sprintf("%s#analyze/%v/%v", serverURL, parentGUID, 1)
		}
	}
}

func fixupMessageType(routingKey string) string {
	if feedParserRegex.MatchString(routingKey) {
		return fmt.Sprintf("feed.%s", feedParserRegex.FindStringSubmatch(routingKey)[2])
	}
	return routingKey
}

func PrettyPrintMap(msg map[string]interface{}) {
	b, err := json.MarshalIndent(msg, "", "  ")
	if err != nil {
		fmt.Println("error:", err)
	}
	fmt.Print(string(b))
}

func (jsp *JsonMessageProcessor) ProcessJSONMessage(msg map[string]interface{}, routingKey string) ([]map[string]interface{}, error) {
	msg["type"] = fixupMessageType(routingKey)
	jsp.fixupMessage(routingKey, msg)

	msgs := make([]map[string]interface{}, 0, 1)

	// explode watchlist/feed hit messages that include a "docs" array
	if val, ok := msg["docs"]; ok {
		subdocs := deepcopy.Iface(val).([]interface{})
		delete(msg, "docs")
		for _, submsg := range subdocs {
			submsg := submsg.(map[string]interface{})
			newMsg := deepcopy.Iface(msg).(map[string]interface{})
			//newSlice := make([]map[string]interface{}, 0, 1)
			//newDoc := deepcopy.Iface(submsg).(map[string]interface{})
			//jsp.fixupMessage(routingKey, newDoc)
			//newSlice = append(newSlice, newDoc)
			//old way newMsg["docs"] = newSlice
			handleKeyValues(submsg)
			for k, v := range submsg {
				if k != "event_timestamp" && k != "cb_version" {
					newMsg[k] = v
				}
			}
			msgs = append(msgs, newMsg)
		}
	} else {
		msgs = append(msgs, msg)
	}

	return msgs, nil
}

/*
 * Used to perform postprocessing on messages.  For exmaple, for feed hits we need to grab the report_title.
 * To do this we must query the Cb Response Server's REST API to get the report_title.  NOTE: In order to do this
 * functionality we need the Cb Response Server URL and API Token set within the config.
 */
func (jsp *JsonMessageProcessor) PostprocessJSONMessage(msg map[string]interface{}) map[string]interface{} {

	feedID, feedIDPresent := msg["feed_id"]
	reportID, reportIDPresent := msg["report_id"]

	/*
		:/p			 * First make sure these fields are present
	*/
	if feedIDPresent && reportIDPresent {
		/*
		 * feedID should be of type json.Number which is typed as a string
		 * reportID should be of type string as well
		 */
		if reflect.TypeOf(feedID).Kind() == reflect.String &&
			reflect.TypeOf(reportID).Kind() == reflect.String {
			iFeedID, err := feedID.(json.Number).Int64()
			if err == nil {
				/*
				 * Get the report_title for this feed hit
				 */
				reportTitle, reportScore, reportLink, err := jsp.CbAPI.GetReport(int(iFeedID), reportID.(string))
				log.Debugf("Report title = %s , Score = %d, link = %s", reportTitle, reportScore, reportLink)
				if err == nil {
					/*
					 * Finally save the report_title into this message
					 */
					msg["report_title"] = reportTitle
					msg["report_score"] = reportScore
					msg["report_link"] = reportLink
					/*
						log.Infof("report title for id %s:%s == %s\n",
							feedID.(json.Number).String(),
							reportID.(string),
							reportTitle)
					*/
				}

			} else {
				log.Info("Unable to convert feed_id to int64 from json.Number")
			}

		} else {
			log.Info("Feed Id was an unexpected type")
		}
	}
	return msg
}

func MarshalJSON(msgs []map[string]interface{}) (string, error) {
	var ret string

	for _, msg := range msgs {
		//msg["cb_server"] = "cbserver"
		marshaled, err := json.Marshal(msg)
		if err != nil {
			return "", err
		}
		ret += string(marshaled) + "\n"
	}

	return ret, nil
}

func (jsp *JsonMessageProcessor) ProcessJSON(routingKey string, indata []byte) ([]map[string]interface{}, error) {
	var msg map[string]interface{}

	decoder := json.NewDecoder(bytes.NewReader(indata))

	// Ensure that we decode numbers in the JSON as integers and *not* float64s
	decoder.UseNumber()

	if err := decoder.Decode(&msg); err != nil {
		return nil, err
	}

	msgs, err := jsp.ProcessJSONMessage(msg, routingKey)
	if err != nil {
		return nil, err
	}
	return msgs, nil
}
