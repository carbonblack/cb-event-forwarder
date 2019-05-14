package tests

import (
	"encoding/json"
	"github.com/carbonblack/cb-event-forwarder/internal/jsonmessageprocessor"
	"testing"
)

func TestJsonProcessing(t *testing.T) {
	exampleJSONInput := `{"cb_server":"None","cb_version":"5.1.0.150914.1400","docs":[{"cb_version":510,"company_name":"Microsoft Corporation","copied_mod_len":9374208,"digsig_publisher":"Microsoft Corporation","digsig_result":"Signed","digsig_result_code":"0","digsig_sign_time":"2014-02-24T12:19:00Z","endpoint":["JASON-WIN81-VM|1"],"file_desc":"Windows Media Player Resources","file_version":"12.0.9600.16384 (winblue_rtm.130821-1623)","group":["Default Group"],"host_count":1,"internal_name":"wmploc.dll","is_64bit":false,"is_executable_image":false,"last_seen":"2015-11-13T20:54:09.144Z","legal_copyright":"© Microsoft Corporation. All rights reserved.","md5":"6403B9CB0267A6EAB6950DEA178C6121","observed_filename":["c:\\windows\\syswow64\\wmploc.dll"],"orig_mod_len":9374208,"original_filename":"wmploc.dll.mui","os_type":"Windows","product_name":"Microsoft® Windows® Operating System","product_version":"12.0.9600.16384","server_added_timestamp":"2015-11-13T20:54:09.144Z","signed":"Signed","timestamp":"2015-11-13T20:54:09.144Z"}, {"cb_version":510,"company_name":"Microsoft Corporation","copied_mod_len":11760128,"digsig_publisher":"Microsoft Corporation","digsig_result":"Signed","digsig_result_code":"0","digsig_sign_time":"2013-08-22T16:53:00Z","endpoint":["JASON-WIN81-VM|1"],"file_desc":"Windows Media Player","file_version":"12.0.9600.16384 (winblue_rtm.130821-1623)","group":["Default Group"],"host_count":1,"internal_name":"wmp.dll","is_64bit":false,"is_executable_image":false,"last_seen":"2015-11-13T20:54:09.14Z","legal_copyright":"© Microsoft Corporation. All rights reserved.","md5":"CA1A0BDF3293CDBE690EEF19E7661D70","observed_filename":["c:\\windows\\syswow64\\wmp.dll"],"orig_mod_len":11760128,"original_filename":"wmp.dll","os_type":"Windows","product_name":"Microsoft® Windows® Operating System","product_version":"12.0.9600.16384","server_added_timestamp":"2015-11-13T20:54:09.14Z","signed":"Signed","timestamp":"2015-11-13T20:54:09.14Z"}],"server_name":"cbtest","timestamp":1447448405.22,"type":"watchlist.hit.binary","watchlist_id":3,"watchlist_name":"Newly Loaded Modules"}`
    jsmp := jsonmessageprocessor.JsonMessageProcessor{UseTimeFloat: true}
	msgs, err := jsmp.ProcessJSON("watchlist.hit.test", []byte(exampleJSONInput))
	if err == nil {
		for _, msg := range msgs {
			_, _ = json.Marshal(msg)
		}
	} else {
		t.Errorf("Error marshaling %s to JSON: %s", exampleJSONInput, err)
	}
}
