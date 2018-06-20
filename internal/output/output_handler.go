package output

import (
	"os"
	conf "github.com/carbonblack/cb-event-forwarder/internal/config"
	"crypto/tls"
	"errors"
	"plugin"
	log "github.com/sirupsen/logrus"
	"path"

)

type OutputHandler interface {
	Go(messages <-chan string, errorChan chan<- error, controlchan <-chan os.Signal) error
	String() string
	Statistics() interface{}
	Key() string
}

func loadOutputFromPlugin(pluginPath string, pluginName string, cfg map[interface{}] interface{}) (OutputHandler, error) {
	log.Infof("loadOutputFromPlugin: Trying to load plugin %s at %s", pluginName, pluginPath)
	plug, err := plugin.Open(path.Join(pluginPath, pluginName+".so"))
	if err != nil {
		log.Panic(err)
	}
	pluginHandlerFuncRaw, err := plug.Lookup("GetOutputHandler")
	if err != nil {
		log.Panicf("Failed to load plugin %v", err)
	}
	return pluginHandlerFuncRaw.(func(cfg map[interface{}] interface{}) (OutputHandler,error))(cfg)
}

func GetOutputsFromCfg (cfg map [interface{}] interface{}) ([]OutputHandler , error) {
		var temp [] OutputHandler = make([] OutputHandler, len (cfg))
		var tlsConfig * tls.Config = nil
		var count int = 0
		for outputtype , output := range cfg {
			tlsConfig = nil
			outputMap, _ := output.(map[interface{}] interface{})
			var tempOH OutputHandler
			switch outputtype.(string) {
			case	"file":
				path,ok := outputMap["path"]
				if  !ok {
					return temp, errors.New("Couldn't find path in file output section")
				}
				fo := NewFileOutputHandler(path.(string))
				tempOH = &fo
			case	"syslog":
				connectionString, ok := outputMap["connection"]
				if !ok {
				}
				if tlsCfg, ok := outputMap["tls"]; ok {
					tlsConfig , _ = conf.GetTLSConfigFromCfg(tlsCfg.(map[interface{}] interface{}))
				} else {
					return temp, errors.New("Couldn't find connection in syslog output section")
				}
				so , err  := NewSyslogOutput(connectionString.(string),tlsConfig)
				if err == nil {
					tempOH = & so
				} else {
					return temp, err
				}

			case	"socket":
				connectionString, ok := outputMap["connection"]
				if !ok {
					return temp, errors.New("Coudn't find connection in socket output section")
				}
				no,err := NewNetOutputHandler(connectionString.(string))
				if err != nil {
					return temp, err
				}
				tempOH = &no
			case	"http":
				if tlsCfg, ok := outputMap["tls"]; ok {
					tlsConfig,_ = conf.GetTLSConfigFromCfg(tlsCfg.(map[interface{}] interface{}))
				}
				//bundle_size_max,bundle_send_timeout, upload_empty_files
				var bundle_size_max,bundle_send_timeout int64
				var upload_empty_files bool
				if b,ok := outputMap["upload_empty_files"]; ok {
					upload_empty_files = b.(bool)
				}
				if t,ok := outputMap["bundle_size_max"]; ok {
					bundle_size_max = t.(int64)
				}
				if t,ok := outputMap["upload_empty_files"]; ok {
					bundle_send_timeout = t.(int64)
				}
				httpBundleBehavior,err := HTTPBehaviorFromCfg(outputMap,true,"/tmp",tlsConfig)
				if err == nil {
					bo, err := NewBundledOutput(bundle_size_max,bundle_send_timeout,upload_empty_files,true,"/tmp",httpBundleBehavior)
					if err != nil {
						return temp, err
					}
					tempOH = &bo
				}
			case 	"s3":
				var bundle_size_max,bundle_send_timeout int64
				var upload_empty_files bool
				if b,ok := outputMap["upload_empty_files"]; ok {
					upload_empty_files = b.(bool)
				}
				if t,ok := outputMap["bundle_size_max"]; ok {
					bundle_size_max = t.(int64)
				}
				if t,ok := outputMap["upload_empty_files"]; ok {
					bundle_send_timeout = t.(int64)
				}
				s3BundleBehavior,err := S3BehaviorFromCfg(outputMap)
				if err == nil {
					bo, err := NewBundledOutput(bundle_size_max,bundle_send_timeout,upload_empty_files,true,"/tmp",&s3BundleBehavior)
					if err != nil {
						return temp, err
					}
					tempOH = &bo
				}
			case	"plugin":
				log.Infof("plugin outputmap = %s", outputMap)
				path,ok := outputMap["path"].(string)
				if  !ok {
					return temp, errors.New("Couldn't find path in plugin output section")
				}
				name,ok := outputMap["name"].(string)
				if  !ok {
					return temp, errors.New("Couldn't find plugin name in plugin output section")
				}

				cfg := make(map[interface{}]interface{})
				if cm,ok := outputMap["config"]; ok {
					if c, ok := cm.(map[interface{}] interface{}); ok  {
						cfg = c
					} else {
						log.Infof("failed to conver plugin config")
						switch t := cm.(type) {
							default:
								log.Infof("real type is %T for %s",t,c)
						}

					}
				}
				ohp,_ := loadOutputFromPlugin(path,name,cfg)
				tempOH = ohp
			default:
				return temp,nil
			}
			temp[count] = tempOH
			count++
		}
		return temp, nil
}