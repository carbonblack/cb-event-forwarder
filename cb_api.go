package main

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
)

type ThreatReport struct {
	FeedId       int         `json:"feed_id"`
	Timestamp    int         `json:"timestamp"`
	FeedCategory string      `json:"feed_category"`
	CreateTime   int         `json:"create_time"`
	Link         string      `json:"link"`
	Title        string      `json:"title"`
	HasQuery     bool        `json:"has_query"`
	IOCs         interface{} `json:"iocs"`
	IsIgnored    bool        `json:"is_ignored"`
	FeedName     string      `json:"feed_name"`
	Score        int         `json:"score"`
}

/*
{u'banningEnabled': True,
 u'binaryOrder': u'',
 u'binaryPageSize': 10,
 u'cblrEnabled': False,
 u'cloud_install': False,
 u'features': {u'thirdparty_sharing': False},
 u'linuxInstallerExists': True,
 u'liveResponseAutoAttach': True,
 u'maxRowsSolrReportQuery': 10000,
 u'maxSearchResultRows': 1000,
 u'osxInstallerExists': True,
 u'processOrder': u'',
 u'processPageSize': 10,
 u'searchExportCount': 1000,
 u'timestampDeltaThreshold': 5,
 u'vdiGloballyEnabled': False,
 u'version': u'5.2.0.161004.1206',
 u'version_release': u'5.2.0-4'}
*/
type ApiInfo struct {
	Version string `json:"version"`
}

func GetCb(route string) ([]byte, error) {
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: !config.CbAPIVerifySSL},
	}

	httpClient := &http.Client{Transport: tr}
	req, err := http.NewRequest("GET", fmt.Sprintf("%s%s", config.CbServerURL, route), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Add("X-Auth-Token", config.CbAPIToken)

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	return body, err
}

func GetCbVersion() (version string, err error) {
	cbInfo := ApiInfo{}

	body, err := GetCb("/api/info")
	if err != nil {
		return
	}

	err = json.Unmarshal(body, &cbInfo)
	version = cbInfo.Version

	return
}

func GetReportTitle(FeedId int, ReportId string) (string, error) {
	threatReport := ThreatReport{}

	body, err := GetCb(fmt.Sprintf("/api/v1/feed/%d/report/%s", FeedId, ReportId))
	if err != nil {
		return "", err
	}

	err = json.Unmarshal(body, &threatReport)

	if err != nil {
		return "", err
	}

	return threatReport.Title, nil
}
