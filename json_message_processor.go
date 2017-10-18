package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/carbonblack/cb-event-forwarder/deepcopy"
	"log"
	"net/url"
	"reflect"
	"regexp"
	"strconv"
	"strings"
)

var feedParserRegex = regexp.MustCompile(`^feed\.(\d+)\.(.*)$`)

func parseFullGuid(v string) (string, int, error) {


	var segmentNumber int64
	var err error

	segmentNumber = 1

	log.Printf("parseFUllGuid : v  = %s\n", v)

	switch {
	case len(v) < 36:
		return v, int(segmentNumber), errors.New("Truncated GUID")
	case len(v) == 36:
		return v, int(segmentNumber), nil
	case len(v) == 45:
		segmentNumber, err = strconv.ParseInt(v[37:], 16, 32)
		log.Printf("segmentNumber = %d\n",segmentNumber)
		if err != nil {
			segmentNumber = 1
		}
	default:
		err = errors.New("Truncated GUID")
	}

	log.Printf("segmentNumber = %d\n",segmentNumber)

	return v[:36], int(segmentNumber), err
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

func fixupMessage(messageType string, msg map[string]interface{}) {
	// go through each key and fix up as necessary

	log.Printf("fixupMessage %s", messageType)

	for key, value := range msg {
		switch {
		case key == "highlights":
			delete(msg, "highlights")
		case key == "event_timestamp":
			msg["timestamp"] = value
			delete(msg, "event_timestamp")
		case key == "hostname":
			msg["computer_name"] = value
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
				ioc_type := value.(map[string]interface{})
				if md5value, ok := ioc_type["md5"]; ok {
					if md5, ok := md5value.(string); ok {
						if len(md5) == 32 {
							ioc_type["md5"] = strings.ToUpper(md5)
						}
					}
				}
			} else {
				if ioc_type, ok := value.(string); ok {
					if ioc_type == "query" {
						// decode the IOC query
						if raw_ioc_value, ok := msg["ioc_value"].(string); ok {
							var ioc_value map[string]string
							if json.Unmarshal([]byte(raw_ioc_value), &ioc_value) == nil {
								if queryIndex, rawQuery, err := parseQueryString(ioc_value); err == nil {
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
					msg[key] = GetIPv4AddressSigned(int32(ipaddr))
				}
			}
		}
	}

	hasProcessGUID := false

	// figure out the canonical process guid associated with this message
	if !strings.HasPrefix(messageType, "alert.") {
		if value, ok := msg["unique_id"]; ok {
			if uniqueId, ok := value.(string); ok {
				processGuid, segment, err := parseFullGuid(uniqueId)

				if err == nil {
					    msg["process_guid"] = processGuid
					    msg["segment_id"] = fmt.Sprintf("%v", segment)
					    hasProcessGUID = true
			    }

			}
		}
	}

	log.Printf("hasProcessGuid = %s",hasProcessGUID)

	// fall back to process_id in the message
	if !hasProcessGUID {
        log.Println("In !hasProcessGUID")
		if value, ok := msg["process_id"]; ok {
			if uniqueId, ok := value.(string); ok {
			    log.Println("UniqueID ok")
			    if segment, ok := msg["segment_id"] ; ok {
	                log.Println("Got segment ok")
	                uniqueId += "-"  + segment.(string)
	                log.Println("uniqueId is now : %s",uniqueId)
	            }

				processGuid, segment, _ := parseFullGuid(uniqueId)
				msg["process_guid"] = processGuid
				msg["segment_id"] = fmt.Sprintf("%v", segment)
				hasProcessGUID = true
			}
		}
	}

	// also deal with parent links
	if value, ok := msg["parent_unique_id"]; ok {
		if uniqueId, ok := value.(string); ok {
			processGuid, segment, _ := parseFullGuid(uniqueId)
			msg["parent_guid"] = processGuid
			msg["parent_segment_id"] = fmt.Sprintf("%v", segment)
		}
	}

	// add deep links back into the Cb web UI if configured
	if config.CbServerURL != "" {
		AddLinksToMessage(messageType, config.CbServerURL, msg)
	}
}

func AddLinksToMessage(messageType, serverURL string, msg map[string]interface{}) {
	// add sensor links when applicable
	if value, ok := msg["sensor_id"]; ok {
		if value, ok := value.(json.Number); ok {
			hostId, err := strconv.ParseInt(value.String(), 10, 32)
			if err == nil {
				msg["link_sensor"] = fmt.Sprintf("%s#/host/%d", serverURL, hostId)
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
	if processGuid, ok := msg["process_guid"]; ok {
		if segmentId, ok := msg["segment_id"]; ok {
			msg["link_process"] = fmt.Sprintf("%s#analyze/%v/%v", serverURL, processGuid, segmentId)
		} else {
			msg["link_process"] = fmt.Sprintf("%s#analyze/%v/%v", serverURL, processGuid, 1)
		}
	}

	if parentGuid, ok := msg["parent_guid"]; ok {
		if segmentId, ok := msg["parent_segment_id"]; ok {
			msg["link_parent"] = fmt.Sprintf("%s#analyze/%v/%v", serverURL, parentGuid, segmentId)
		} else {
			msg["link_parent"] = fmt.Sprintf("%s#analyze/%v/%v", serverURL, parentGuid, 1)
		}
	}
}

func fixupMessageType(routingKey string) string {
	if feedParserRegex.MatchString(routingKey) {
		return fmt.Sprintf("feed.%s", feedParserRegex.FindStringSubmatch(routingKey)[2])
	} else {
		return routingKey
	}
}

func PrettyPrintMap(msg map[string]interface{}){
	b, err := json.MarshalIndent(msg, "", "  ")
	if err != nil {
		fmt.Println("error:", err)
	}
	fmt.Print(string(b))
}

func ProcessJSONMessage(msg map[string]interface{}, routingKey string) ([]map[string]interface{}, error) {
	msg["type"] = fixupMessageType(routingKey)
	fixupMessage(routingKey, msg)

	msgs := make([]map[string]interface{}, 0, 1)

	// explode watchlist/feed hit messages that include a "docs" array
	if val, ok := msg["docs"]; ok {
		subdocs := deepcopy.Iface(val).([]interface{})
		delete(msg, "docs")

		for _, submsg := range subdocs {
			submsg := submsg.(map[string]interface{})
			newMsg := deepcopy.Iface(msg).(map[string]interface{})
			newSlice := make([]map[string]interface{}, 0, 1)
			newDoc := deepcopy.Iface(submsg).(map[string]interface{})
			fixupMessage(routingKey, newDoc)
			newSlice = append(newSlice, newDoc)
			newMsg["docs"] = newSlice
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
func PostprocessJSONMessage(msg map[string]interface{}) map[string]interface{} {

	if val, ok := msg["type"]; ok {
		messageType := val.(string)

		if strings.HasPrefix(messageType, "feed.") {
			feedId, feedIdPresent := msg["feed_id"]
			reportId, reportIdPresent := msg["report_id"]

			/*
			 * First make sure these fields are present
			 */
			if feedIdPresent && reportIdPresent {
				/*
				 * feedId should be of type json.Number which is typed as a string
				 * reportId should be of type string as well
				 */
				if reflect.TypeOf(feedId).Kind() == reflect.String &&
					reflect.TypeOf(reportId).Kind() == reflect.String {
					iFeedId, err := feedId.(json.Number).Int64()
					if err == nil {
						/*
						 * Get the report_title for this feed hit
						 */
						reportTitle, reportScore, err := GetReport(int(iFeedId), reportId.(string))
						log.Printf("Report title = %s , Score = %s",reportTitle, reportScore)
						if err == nil {
							/*
							 * Finally save the report_title into this message
							 */
							msg["report_title"] = reportTitle
							msg["report_score"] = reportScore
							/*
							log.Printf("report title,score for id %s:%s == %s,%s\n",
								feedId.(json.Number).String(),
								reportId.(string),
								reportTitle,
								reportScore)
								*/
						}

					} else {
						log.Println("Unable to convert feed_id to int64 from json.Number")
					}

				} else {
					log.Println("Feed Id was an unexpected type")
				}
			}
		}

	}
	return msg
}
