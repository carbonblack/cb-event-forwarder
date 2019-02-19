package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/carbonblack/cb-event-forwarder/internal/deepcopy"
	log "github.com/sirupsen/logrus"
	"net/url"
	"reflect"
	"regexp"
	"strconv"
	"strings"
)

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

func fixupMessage(messageType string, msg map[string]interface{}) {
	// go through each key and fix up as necessary

	for key, value := range msg {
		switch {
		case key == "highlights":
			delete(msg, "highlights")
		case key == "event_timestamp":
			if config.UseTimeFloat {
				msg["timestamp"] = value
			} else {
				if fv,ok := value.(json.Number); ok {
					msg["timestamp"] = fv.String()
				}
			}
			delete(msg, "event_timestamp")
		case key == "timestamp":
			if config.UseTimeFloat {
				msg["timestamp"] = value
			} else {
				if fv,ok := value.(json.Number); ok {
					msg["timestamp"] = fv.String()
				}
			}
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
					msg[key] = GetIPv4AddressSigned(int32(ipaddr))
				}
			}
		}
	}

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
		}
	}

	// also deal with parent links
	if value, ok := msg["parent_unique_id"]; ok {
		if uniqueID, ok := value.(string); ok {
			processGUID, segment, _ := parseFullGUID(uniqueID)
			msg["parent_guid"] = processGUID
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

		var reportIDString string

		if strings.HasPrefix(messageType, "feed.") {
			reportIDString = "report_id"
		} else if strings.HasPrefix(messageType, "alert.") {
			reportIDString = "watchlist_id"
		} else {
			return msg
		}

		feedID, feedIDPresent := msg["feed_id"]
		reportID, reportIDPresent := msg[reportIDString]

		/*
		 * First make sure these fields are present
		 */
		if feedIDPresent && reportIDPresent {
			/*
			 * feedID should be of type json.Number which is typed as a string
			 * reportID should be of type string as well
			 */
			if reflect.TypeOf(feedID).Kind() == reflect.String &&
				reflect.TypeOf(reportID).Kind() == reflect.String {
				iFeedID, err := feedID.(json.Number).Int64()
				if err == nil && iFeedID != -1 {
					/*
					 * Get the report_title for this feed hit
					 */
					reportTitle, reportScore, reportLink, err := GetReport(int(iFeedID), reportID.(string))
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
					log.Debug("Unable to convert feed_id to int64 from json.Number")
				}

			} else {
				log.Debug("Feed Id was an unexpected type")
			}
		}
	}
	return msg
}
