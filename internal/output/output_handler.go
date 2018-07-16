package output

import (
	"crypto/tls"
	"errors"
	conf "github.com/carbonblack/cb-event-forwarder/internal/config"
	"github.com/carbonblack/cb-event-forwarder/internal/encoder"
	log "github.com/sirupsen/logrus"
	"os"
	"path"
	"plugin"
	"strconv"
	"sync"
	"time"
)

type OutputHandler interface {
	Go(messages <-chan map[string]interface{}, errorChan chan<- error, controlchan <-chan os.Signal, wg sync.WaitGroup) error
	String() string
	Statistics() interface{}
	Key() string
}

func loadOutputFromPlugin(pluginPath string, pluginName string, cfg map[interface{}]interface{}, e encoder.Encoder) (OutputHandler, error) {
	log.Infof("loadOutputFromPlugin: Trying to load plugin %s at %s", pluginName, pluginPath)
	plug, err := plugin.Open(path.Join(pluginPath, pluginName+".so"))
	if err != nil {
		log.Panic(err)
	}
	pluginHandlerFuncRaw, err := plug.Lookup("GetOutputHandler")
	if err != nil {
		log.Panicf("Failed to load plugin %v", err)
	}
	return pluginHandlerFuncRaw.(func(map[interface{}]interface{}, encoder.Encoder) (OutputHandler, error))(cfg, e)
}

func GetOutputsFromCfg(cfg []interface{}) ([]OutputHandler, error) {
	var temp []OutputHandler = make([]OutputHandler, len(cfg))
	var tlsConfig *tls.Config = nil
	var count int = 0
	for _, outputI := range cfg {
		if outputCfg, ok := outputI.(map[interface{}]interface{}); ok {
			for outputtype, output := range outputCfg {
				tlsConfig = nil
				outputMap, _ := output.(map[interface{}]interface{})
				var tempOH OutputHandler
				var err error = nil
				var myencoder encoder.Encoder
				if t, ok := outputMap["format"]; ok {
					myencoder, err = encoder.GetEncoderFromCfg(t.(map[interface{}]interface{}))
				} else {
					return temp, err
				}

				switch outputtype.(string) {
				case "file":
					path, ok := outputMap["path"]
					if !ok {
						return temp, errors.New("Couldn't find path in file output section")
					}
					var rolloverSizeBytes int64 = 500000
					var rolloverDuration time.Duration
					rollOverDuration, ok := outputMap["rollover_duration"]
					if ok {
						rolloverDuration = time.Duration(rollOverDuration.(int)) * time.Second

					} else {
						rolloverDuration = time.Duration(86400) * time.Second
					}
					rollOverSizeBytes, ok := outputMap["rollover_size_bytes"]
					if ok {
						s, err := strconv.ParseInt(rollOverSizeBytes.(string), 10, 64)
						if err != nil {
							log.Panicf("%v", err)
						}
						rolloverSizeBytes = s
					}
					fo := NewFileOutputHandler(path.(string), rolloverSizeBytes, rolloverDuration, myencoder)
					tempOH = &fo
				case "syslog":
					connectionString, ok := outputMap["connection"]
					if !ok {
					}
					if tlsCfg, ok := outputMap["tls"]; ok {
						tlsConfig, _ = conf.GetTLSConfigFromCfg(tlsCfg.(map[interface{}]interface{}))
					} else {
						return temp, errors.New("Couldn't find connection in syslog output section")
					}
					so, err := NewSyslogOutput(connectionString.(string), tlsConfig, myencoder)
					if err == nil {
						tempOH = &so
					} else {
						return temp, err
					}

				case "socket":
					connectionString, ok := outputMap["connection"]
					if !ok {
						return temp, errors.New("Coudn't find connection in socket output section")
					}
					no, err := NewNetOutputHandler(connectionString.(string), myencoder)
					if err != nil {
						return temp, err
					}
					tempOH = &no
				case "http":
					if tlsCfg, ok := outputMap["tls"]; ok {
						tlsConfig, _ = conf.GetTLSConfigFromCfg(tlsCfg.(map[interface{}]interface{}))
					}
					//bundle_size_max,bundle_send_timeout, upload_empty_files
					var bundle_size_max, bundle_send_timeout int64
					var upload_empty_files bool
					if b, ok := outputMap["upload_empty_files"]; ok {
						upload_empty_files = b.(bool)
					}
					if t, ok := outputMap["bundle_size_max"]; ok {
						s, err := strconv.ParseInt(t.(string), 10, 64)
						if err != nil {
							log.Panicf("%v", err)
						}
						bundle_size_max = s
					}
					if t, ok := outputMap["bundle_send_timeout"]; ok {
						s, err := strconv.ParseInt(t.(string), 10, 64)
						if err != nil {
							log.Panicf("%v", err)
						}
						bundle_send_timeout = s
					}
					httpBundleBehavior, err := HTTPBehaviorFromCfg(outputMap, true, "/tmp", tlsConfig)
					if err == nil {
						bo, err := NewBundledOutput("/var/cb/data/event-forwarder", bundle_size_max, bundle_send_timeout, upload_empty_files, true, "/tmp", httpBundleBehavior, myencoder)
						if err != nil {
							return temp, err
						}
						tempOH = &bo
					}
				case "splunk":
					if tlsCfg, ok := outputMap["tls"]; ok {
						tlsConfig, _ = conf.GetTLSConfigFromCfg(tlsCfg.(map[interface{}]interface{}))
					}
					//bundle_size_max,bundle_send_timeout, upload_empty_files
					var bundle_size_max, bundle_send_timeout int64
					var upload_empty_files bool
					if b, ok := outputMap["upload_empty_files"]; ok {
						upload_empty_files = b.(bool)
					}
					if t, ok := outputMap["bundle_size_max"]; ok {
						s, err := strconv.ParseInt(t.(string), 10, 64)
						if err != nil {
							log.Panicf("%v", err)
						}
						bundle_size_max = s
					}
					if t, ok := outputMap["bundle_send_timeout"]; ok {
						s, err := strconv.ParseInt(t.(string), 10, 64)
						if err != nil {
							log.Panicf("%v", err)
						}
						bundle_send_timeout = s
					}
					splunkBundleBehavior, err := SplunkBehaviorFromCfg(outputMap, true, "/tmp", tlsConfig)
					if err == nil {
						bo, err := NewBundledOutput("/var/cb/data/event-forwarder", bundle_size_max, bundle_send_timeout, upload_empty_files, true, "/tmp", splunkBundleBehavior, myencoder)
						if err != nil {
							return temp, err
						}
						tempOH = &bo
					}
				case "s3":
					var bundle_size_max, bundle_send_timeout int64
					var upload_empty_files bool
					if b, ok := outputMap["upload_empty_files"]; ok {
						upload_empty_files = b.(bool)
					}
					if t, ok := outputMap["bundle_size_max"]; ok {
						s, err := strconv.ParseInt(t.(string), 10, 64)
						if err != nil {
							log.Panicf("%v", err)
						}
						bundle_size_max = s
					}
					if t, ok := outputMap["bundle_send_timeout"]; ok {
						s, err := strconv.ParseInt(t.(string), 10, 64)
						if err != nil {
							log.Panicf("%v", err)
						}
						bundle_send_timeout = s
					}
					s3BundleBehavior, err := S3BehaviorFromCfg(outputMap)
					if err == nil {
						bo, err := NewBundledOutput("/var/cb/data/event-forwarder", bundle_size_max, bundle_send_timeout, upload_empty_files, true, "/tmp", &s3BundleBehavior, myencoder)
						if err != nil {
							return temp, err
						}
						tempOH = &bo
					}
				case "plugin":
					log.Debugf("plugin outputmap = %s", outputMap)
					path, ok := outputMap["path"].(string)
					if !ok {
						return temp, errors.New("Couldn't find path in plugin output section")
					}
					name, ok := outputMap["name"].(string)
					if !ok {
						return temp, errors.New("Couldn't find plugin name in plugin output section")
					}

					cfg := make(map[interface{}]interface{})
					if cm, ok := outputMap["config"]; ok {
						if c, ok := cm.(map[interface{}]interface{}); ok {
							cfg = c
						} else {
							log.Errorf("failed to convert plugin config")
							switch t := cm.(type) {
							default:
								log.Errorf("real type is %T for %s", t, c)
							}

						}
					}
					ohp, _ := loadOutputFromPlugin(path, name, cfg, myencoder)
					tempOH = ohp
				default:
					return temp, nil
				}
				temp[count] = tempOH
				count++
				break
			}
		}
	}
	return temp, nil
}
