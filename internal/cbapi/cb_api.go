package cbapi

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	conf "github.com/carbonblack/cb-event-forwarder/internal/config"
	"io/ioutil"
	"net/http"
	"net/url"
	"strconv"
	"time"
	"zvelo.io/ttlru"
	"errors"
)

type ThreatReport struct {
	FeedID       int         `json:"feed_id"`
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

type APIInfo struct {
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
var FeedCache = ttlru.New(128, ttlru.WithTTL(5*time.Minute))

type CbAPIHandler struct {
	Tls * tls.Config
	CbAPIProxyURL string
	CbServerURL string
	CbAPIToken string
}

func CbAPIHandlerFromCfg(cfg map[interface{}] interface{}) (*CbAPIHandler , error ) {
	 var tls  * tls.Config = nil
	var err error
	if postProcessing, ok  := cfg["post_process"]; ok && postProcessing.(bool){
		if tlsCfgi, ok := cfg["tls"]; ok {
			tlsCfg, ok := tlsCfgi.(map[interface{}]interface{})
			if ok {
				t, e := conf.GetTLSConfigFromCfg(tlsCfg)
				if e != nil {
					return nil, e
				}
				tls = t
			}
		}
		api_proxy_url := ""
		if t, ok := cfg["proxy_url"]; ok {
			api_proxy_url = t.(string)
		}
		server_url := ""
		if t, ok := cfg["cb_server_url"]; ok {
			api_proxy_url = t.(string)
		}
		api_token := ""
		if t, ok := cfg["api_token"]; ok {
			api_token = t.(string)
		} else {
			err = errors.New("Must provide api_token to cbapi post processor")
		}
		return NewCbAPIHandler(server_url,api_token,api_proxy_url,tls), err
	}
	return nil, nil
}

func NewCbAPIHandler(cbServerURL,cbAPIToken, cbAPIProxyURL string , tls * tls.Config) *CbAPIHandler {
	return &CbAPIHandler{Tls:tls , CbAPIProxyURL:cbAPIProxyURL, CbServerURL:cbServerURL, CbAPIToken :cbAPIToken}
}

func (cbapi *CbAPIHandler) GetCb(route string) ([]byte, error) {

	var proxyRequest func(*http.Request) (*url.URL, error)

	if cbapi.CbAPIProxyURL == "" {
		/*
		 * No Proxy was specified
		 */
		proxyRequest = nil
	} else {
		proxyURL, err := url.Parse(cbapi.CbAPIProxyURL)
		if err != nil {
			return nil, err
		}
		proxyRequest = http.ProxyURL(proxyURL)
	}

	tr := &http.Transport{
		TLSClientConfig: cbapi.Tls,
		Proxy:           proxyRequest,
	}

	httpClient := &http.Client{Transport: tr}
	req, err := http.NewRequest("GET", fmt.Sprintf("%s%s", cbapi.CbServerURL, route), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Add("X-Auth-Token", cbapi.CbAPIToken)

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("Cb Response Server returned a %d status code", resp.StatusCode)
	}

	body, err := ioutil.ReadAll(resp.Body)
	return body, err
}

func (cbapi *CbAPIHandler) GetCbVersion() (version string, err error) {
	cbInfo := APIInfo{}

	/*
	 * NOTE: CbAPIServerUrl ends with a '/'
	 */
	body, err := cbapi.GetCb("api/info")
	if err != nil {
		return
	}

	err = json.Unmarshal(body, &cbInfo)
	version = cbInfo.Version

	return
}

func (cbapi *CbAPIHandler) GetReportTitle(FeedID int, ReportID string) (string, error) {
	threatReport := ThreatReport{}

	key := strconv.Itoa(FeedID) + "|" + ReportID

	reportTitle, cachePresent := FeedCache.Get(key)

	if cachePresent && reportTitle != nil {
		return reportTitle.(string), nil
	}
	body, err := cbapi.GetCb(fmt.Sprintf("api/v1/feed/%d/report/%s", FeedID, ReportID))
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

func (cbapi *CbAPIHandler) GetReport(FeedID int, ReportID string) (string, int, string, error) {

	threatReport := ThreatReport{}

	key := strconv.Itoa(FeedID) + "|" + ReportID

	rawThreatReportP, cachePresent := FeedCache.Get(key)

	if cachePresent {
		if rawThreatReportP != nil {
			threatReportP := (rawThreatReportP).(*ThreatReport)
			threatReport := *threatReportP
			reportTitle := threatReport.Title
			reportScore := threatReport.Score
			reportLink := threatReport.Link
			return reportTitle, reportScore, reportLink, nil
		}

	}
	//implicit ELSE
	body, err := cbapi.GetCb(fmt.Sprintf("api/v1/feed/%d/report/%s", FeedID, ReportID))

	if err != nil {
		return "", 0, "", err
	}

	err = json.Unmarshal(body, &threatReport)

	if err != nil {
		return "", 0, "", err
	}

	FeedCache.Set(key, &threatReport)

	return threatReport.Title, threatReport.Score, threatReport.Link, nil

}
