package tests

import (
	"bytes"
	"encoding/json"
	"fmt"
	cef "github.com/carbonblack/cb-event-forwarder/internal/cef"
	"io/ioutil"
	"os"
	"path"
	"testing"
)

func generateCefOutput(exampleJSONInput string) error {
	var msg map[string]interface{}

	decoder := json.NewDecoder(bytes.NewReader([]byte(exampleJSONInput)))

	// Ensure that we decode numbers in the JSON as integers and *not* float64s
	decoder.UseNumber()

	if err := decoder.Decode(&msg); err != nil {
		return err
	}

	msgs, err := jsmp.ProcessJSONMessage(msg, "watchlist.hit.test")
	if err != nil {
		return err
	}

	for i, msg := range msgs {
		if _, err := cef.EncodeWithSeverity(msg, 5); err != nil {
			return fmt.Errorf("Error encoding message %s [index %d]: %s", msg, i, err)
		}
	}

	return nil
}

/*
2015/11/16 20:43:15 Error marshaling message when processing {"server_name": "cbtest", "docs": [{"process_md5": "dd7423abbe2913e70d50e9318ad57ee4", "sensor_id": 1, "modload_count": 54, "parent_unique_id": "00000001-0000-0c64-01d1-20d9b5c66250-00000001", "cmdline": "\"C:\\Program Files (x86)\\Google\\Update\\GoogleUpdate.exe\" /ua /installsource scheduler", "alliance_link_virustotal": "https://www.virustotal.com/file/74bc123808f3fa60addc51c1383f8250608d3dba3a8dc175b3418a1cf0bc53e9/analysis/1445996108/", "filemod_count": 0, "id": "00000001-0000-0f24-01d1-20d9b5cb25d5", "parent_name": "taskeng.exe", "parent_md5": "000000000000000000000000000000", "group": "Default Group", "hostname": "JASON-WIN81-VM", "last_update": "2015-11-17T01:46:00.074Z", "start": "2015-11-17T01:46:00.043Z", "comms_ip": -1407841790, "regmod_count": 1, "interface_ip": -1407842920, "process_pid": 3876, "username": "SYSTEM", "alliance_score_virustotal": 1, "alliance_updated_virustotal": "2015-10-28T01:40:44Z", "process_name": "googleupdate.exe", "path": "c:\\program files (x86)\\google\\update\\googleupdate.exe", "netconn_count": 0, "parent_pid": 3172, "crossproc_count": 0, "segment_id": 1, "host_type": "workstation", "alliance_data_virustotal": ["dd7423abbe2913e70d50e9318ad57ee4"], "os_type": "windows", "childproc_count": 0, "unique_id": "00000001-0000-0f24-01d1-20d9b5cb25d5-00000001"}], "event_timestamp": 1447724404.34, "watchlist_id": 11, "highlights": [], "cb_version": "5.1.0.150914.1400", "watchlist_name": "vt_watchlist"}: could not map docs to []interface{}
*/

func TestCefEncoder(t *testing.T) {
	jsonInputs := [...]string{
		`{"server_name": "cbtest", "docs": [{"process_md5": "dd7423abbe2913e70d50e9318ad57ee4", "sensor_id": 1, "modload_count": 54, "parent_unique_id": "00000001-0000-0c64-01d1-20d9b5c66250-00000001", "cmdline": "\"C:\\Program Files (x86)\\Google\\Update\\GoogleUpdate.exe\" /ua /installsource scheduler", "alliance_link_virustotal": "https://www.virustotal.com/file/74bc123808f3fa60addc51c1383f8250608d3dba3a8dc175b3418a1cf0bc53e9/analysis/1445996108/", "filemod_count": 0, "id": "00000001-0000-0f24-01d1-20d9b5cb25d5", "parent_name": "taskeng.exe", "parent_md5": "000000000000000000000000000000", "group": "Default Group", "hostname": "JASON-WIN81-VM", "last_update": "2015-11-17T01:46:00.074Z", "start": "2015-11-17T01:46:00.043Z", "comms_ip": -1407841790, "regmod_count": 1, "interface_ip": -1407842920, "process_pid": 3876, "username": "SYSTEM", "alliance_score_virustotal": 1, "alliance_updated_virustotal": "2015-10-28T01:40:44Z", "process_name": "googleupdate.exe", "path": "c:\\program files (x86)\\google\\update\\googleupdate.exe", "netconn_count": 0, "parent_pid": 3172, "crossproc_count": 0, "segment_id": 1, "host_type": "workstation", "alliance_data_virustotal": ["dd7423abbe2913e70d50e9318ad57ee4"], "os_type": "windows", "childproc_count": 0, "unique_id": "00000001-0000-0f24-01d1-20d9b5cb25d5-00000001"}], "event_timestamp": 1447724404.34, "watchlist_id": 11, "highlights": [], "cb_version": "5.1.0.150914.1400", "watchlist_name": "vt_watchlist"}`,
		`{"ioc_attr": {}, "sensor_id": 4, "group": "testgroup", "server_name": "localhost", "docs": [{"host_count": 1, "endpoint": "WIN-IA9NQ1GN8OI|4", "watchlist_4": "2015-09-01T18:30:05.934Z", "digsig_result_code": "2148204800", "digsig_result": "Unsigned", "observed_filename": ["c:\\users\\bit9rad\\desktop\\wildfire-test-pe-file.exe"], "copied_mod_len": 55296, "alliance_score_wildfire": 100, "last_seen": "2015-09-04T07:50:20.552Z", "alliance_link_wildfire": "", "server_added_timestamp": "2015-09-01T18:24:47.966Z", "orig_mod_len": 55296, "md5": "449571D58547F434FAF544F2BAF2FA4C", "cb_version": 510, "group": "testgroup", "is_executable_image": true, "alliance_data_wildfire": ["Binary_449571D58547F434FAF544F2BAF2FA4C"], "os_type": "Windows", "is_64bit": false, "alliance_updated_wildfire": "2015-09-01T14:36:50.000Z"}], "hostname": "WIN-IA9NQ1GN8OI", "report_score": 100, "feed_name": "wildfire", "feed_id": 33, "ioc_value": "449571D58547F434FAF544F2BAF2FA4C", "cb_server": "None", "timestamp": 1441439437.029, "cb_version": "5.1.0.150625.0500", "ioc_type": "md5", "md5": "449571D58547F434FAF544F2BAF2FA4C", "type": "feed.storage.hit.binary", "report_id": "Binary_449571D58547F434FAF544F2BAF2FA4C"}`,
		`{"server_name": "cbtest", "docs": [{"host_count": 1, "digsig_result": "Signed", "observed_filename": ["c:\\windows\\system32\\aeinv.dll"], "product_version": "6.3.9600.17415", "signed": "Signed", "digsig_sign_time": "2014-11-08T08:02:00Z", "is_executable_image": false, "orig_mod_len": 537088, "is_64bit": true, "digsig_publisher": "Microsoft Corporation", "group": ["Default Group"], "file_version": "6.3.9600.17415 (winblue_r4.141028-1500)", "company_name": "Microsoft Corporation", "internal_name": "aeinv.dll", "product_name": "Microsoft\u00ae Windows\u00ae Operating System", "digsig_result_code": "0", "timestamp": "2015-11-17T01:34:20.786Z", "copied_mod_len": 537088, "server_added_timestamp": "2015-11-17T01:34:20.786Z", "md5": "18640BDECFFE12E3B2522B081F77FC30", "endpoint": ["WIN-OTEMNUTBS23|7"], "legal_copyright": "\u00a9 Microsoft Corporation. All rights reserved.", "original_filename": "aeinv.dll", "cb_version": 510, "os_type": "Windows", "file_desc": "Application Experience Program Inventory Component", "last_seen": "2015-11-17T01:34:20.786Z"}, {"host_count": 1, "digsig_result": "Signed", "observed_filename": ["c:\\windows\\system32\\aepdu.dll"], "product_version": "6.3.9600.16384", "signed": "Signed", "digsig_sign_time": "2014-11-08T08:02:00Z", "is_executable_image": false, "orig_mod_len": 688128, "is_64bit": true, "digsig_publisher": "Microsoft Corporation", "group": ["Default Group"], "file_version": "6.3.9600.16384 (winblue_rtm.130821-1623)", "company_name": "Microsoft Corporation", "internal_name": "(unknown)", "product_name": "Microsoft\u00ae Windows\u00ae Operating System", "digsig_result_code": "0", "timestamp": "2015-11-17T01:34:20.784Z", "copied_mod_len": 688128, "server_added_timestamp": "2015-11-17T01:34:20.784Z", "md5": "2953772F25ECCF74B04306E94C686CED", "endpoint": ["WIN-OTEMNUTBS23|7"], "legal_copyright": "\u00a9 Microsoft Corporation. All rights reserved.", "original_filename": "(unknown)", "cb_version": 510, "os_type": "Windows", "file_desc": "Program Compatibility Data Updater", "last_seen": "2015-11-17T01:34:20.784Z"}], "event_timestamp": 1447724404.15, "watchlist_id": 3, "highlights": [], "cb_version": "5.1.0.150914.1400", "watchlist_name": "Newly Loaded Modules"}`,
	}

	for _, jsonInput := range jsonInputs {
		if err := generateCefOutput(jsonInput); err != nil {
			t.Errorf("Error generating CEF output for %s: %s", jsonInput, err.Error())
		}
	}
}

func BenchmarkCefEncoder(b *testing.B) {
	fn := path.Join("../test/raw_data/json/watchlist.hit.process/0.json")
	fp, _ := os.Open(fn)
	d, _ := ioutil.ReadAll(fp)
	for i := 0; i < b.N; i++ {
		generateCefOutput(string(d))
	}
}
