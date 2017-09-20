package main

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"github.com/zvelo/ttlru"
	"io/ioutil"
	"net/http"
	"net/url"
	"strconv"
	"time"
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

type ApiInfo struct {
	BanningEnabled          bool        `json:"banningEnabled"`
	BinaryOrder             string      `json:"binaryOrder"`
	BinaryPageSize          int         `json:"binaryPageSize"`
	CBLREnabled             bool        `json:"cblrEnabled"`
	CloudInstall            bool        `json:"cloud_install"`
	Features                interface{} `json:"features"`
	LinuxInstallerExists    bool        `json:"linuxInstallerExists"`
	MaxRowsSolrReportQuery  int         `json:"maxRowsSolrReportQuery"`
	MaxSearchResultRows     int         `json:"maxSearchResultRows"`
	OsxInstallerExists      bool        `json:"osxInstallerExists"`
	ProcessOrder            string      `json:"processOrder"`
	ProcessPageSize         int         `json:"processPageSize"`
	SearchExportCount       int         `json:"searchExportCount"`
	TimestampDeltaThreshold int         `json:"timestampDeltaThreshold"`
	VdiGloballyEnabled      bool        `json:"vdiGloballyEnabled"`
	Version                 string      `json:"version"`
	VersionRelease          string      `json:"version_release"`
}

/*
 * This is the Cache for the report title within post processing
 * Mapping: "<feed_id>|<report_id>" -> report_title
 */
var FeedCache = ttlru.New(128, 5*time.Minute)

func GetCb(route string) ([]byte, error) {

	var proxyRequest func(*http.Request) (*url.URL, error)

	if config.CbAPIProxyUrl == "" {
		/*
		 * No Proxy was specified
		 */
		proxyRequest = nil
	} else {
		proxyUrl, err := url.Parse(config.CbAPIProxyUrl)
		if err != nil {
			return nil, err
		}
		proxyRequest = http.ProxyURL(proxyUrl)
	}

	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: !config.CbAPIVerifySSL, MinVersion: tls.VersionTLS12},
		Proxy:           proxyRequest,
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

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("Cb Response Server returned a %d status code.\n", resp.StatusCode)
	}

	body, err := ioutil.ReadAll(resp.Body)
	return body, err
}

func GetCbVersion() (version string, err error) {
	cbInfo := ApiInfo{}

	/*
	 * NOTE: CbAPIServerUrl ends with a '/'
	 */
	body, err := GetCb("api/info")
	if err != nil {
		return
	}

	err = json.Unmarshal(body, &cbInfo)
	version = cbInfo.Version

	return
}

func GetReportTitle(FeedId int, ReportId string) (string, error) {
	threatReport := ThreatReport{}

	key := strconv.Itoa(FeedId) + "|" + ReportId

	reportTitle, cachePresent := FeedCache.Get(key)

	if cachePresent && reportTitle != nil {
		return reportTitle.(string), nil
	} else {
		body, err := GetCb(fmt.Sprintf("api/v1/feed/%d/report/%s", FeedId, ReportId))
		if err != nil {
			return "", err
		}

		err = json.Unmarshal(body, &threatReport)

		if err != nil {
			return "", err
		}

		FeedCache.Set(key, threatReport.Title)

		return threatReport.Title, nil
	}
}
