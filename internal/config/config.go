package config

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	_ "expvar"
	"fmt"
	log "github.com/sirupsen/logrus"
	"github.com/vaughan0/go-ini"
	"gopkg.in/yaml.v2"
	"io/ioutil"
)

func LoadFile(filename string) (map[string]interface{}, [] map[string] interface{}, error) {
	var globs map[string]interface{} = make(map[string]interface{})
	var conf_array [] map[string] interface{}
	content, err := ioutil.ReadFile(filename)
	if err != nil {
		return globs, conf_array, err
	}
	m := make(map[string]interface{})
	err = yaml.Unmarshal(content, &m)
	if err != nil {
		var i interface{}
		yaml.Unmarshal(content, &i)
		switch t := i.(type) {
		default:
			log.Infof(fmt.Sprintf("Real type of yaml object is %T %s", t, i))
		}
		return globs, conf_array, err
	} else {
		for k, v := range m {
			if k == "event_forwarders" {
				for _, v2 := range v.([]interface{}) {
					imapi, ok := v2.(map[interface{}]interface{})
					if !ok {
						switch t := v2.(type) {
							default:
								log.Infof("Failed to convert in load file %T %s",t,t)
						}
					}
					smapi := make(map[string] interface{})
					for ki,vi := range imapi {
						smapi[ki.(string)] = vi
					}
					conf_array = append(conf_array, smapi)
				}
			} else {
				globs[k] = v
			}
		}

		log.Debugf("Conf array = %s \n globals = %s", conf_array, globs)

		return globs, conf_array, nil
	}
	return globs, conf_array, err
}

func parseCbConf() (username, password string, err error) {
	input, err := ini.LoadFile("/etc/cb/cb.conf")
	if err != nil {
		return username, password, err
	}
	username, _ = input.Get("", "RabbitMQUser")
	password, _ = input.Get("", "RabbitMQPassword")

	if len(username) == 0 || len(password) == 0 {
		return username, password, errors.New("Could not get RabbitMQ credentials from /etc/cb/cb.conf")
	}
	return
}


func ParseConfigs(fn string) (map[string]interface{}, []map[string]interface{}, error) {
	//errs := ConfigurationError{Empty: true}
	globs, configs, err := LoadFile(fn)
	log.Infof("Globs = %s \n configs from load file = %s \n err = %s \n ", globs, configs, err)
	return globs, configs, err
}
	/*
	tempconfigs := make([]Configuration, len(configs))
	for config_index, config := range configs {
		log.Infof("config is key is %s %s", config_index, config.ConfigMap)
		// defaults
		config.DebugFlag = false
		config.OutputFormat = JSONOutputFormat
		config.OutputType = FileOutputType
		config.AMQPHostname = "localhost"
		config.AMQPUsername = "cb"
		config.HTTPServerPort = 33706
		config.AMQPPort = 5004
		config.DebugStore = "/tmp"

		// required values
		val, error := config.GetString("server_name")
		if error != nil {
			config.ServerName = "CB"
		} else {
			config.ServerName = val
		}

		bval, error := config.GetBool("debug")
		if error == nil && bval {
			config.DebugFlag = true
			log.SetLevel(log.DebugLevel)

			customFormatter := new(log.TextFormatter)
			customFormatter.TimestampFormat = "2006-01-02 15:04:05"
			log.SetFormatter(customFormatter)
			customFormatter.FullTimestamp = true

			log.Debug("Debugging output is set to True")
		}

		AddToOutput := make(map[string]string)

		addToOutput, err := config.GetString("add_to_output")
		if err == nil {
			thingsToAdd := strings.Split(addToOutput, ",")

			for _, element := range thingsToAdd {
				keyValToAdd := strings.Split(element, "=")
				if len(keyValToAdd) >= 2 {
					key := strings.TrimSpace(keyValToAdd[0])
					val := strings.TrimSpace(keyValToAdd[1])
					AddToOutput[key] = val
				}
			}
		}
		config.AddToOutput = AddToOutput

		removeFromOutput, err := config.GetString("remove_from_output")
		if err == nil {
			thingsToRemove := strings.Split(removeFromOutput, ",")
			numberOfThingsToRemove := len(thingsToRemove)
			strippedThingsToRemove := make([]string, numberOfThingsToRemove)
			for index, element := range thingsToRemove {
				strippedThingsToRemove[index] = strings.TrimSpace(element)
			}
			if numberOfThingsToRemove > 0 {
				config.RemoveFromOutput = strippedThingsToRemove
			} else {
				config.RemoveFromOutput = make([]string, 0)
			}
		} else {
			config.RemoveFromOutput = make([]string, 0)
		}

		debugStore, err := config.GetString("debug_store")
		if err == nil {
			config.DebugStore = debugStore
		} else {
			config.DebugStore = "/var/log/cb/integrations/cb-event-forwarder"
		}

		log.Debugf("Debug Store is %s", config.DebugStore)

		ival, err := config.GetInt("http_server_port")
		if err == nil {
			config.HTTPServerPort = ival
		}

		val, err = config.GetString("rabbit_mq_username")
		if err == nil {
			config.AMQPUsername = val
		}

		val, err = config.GetString("rabbit_mq_password")
		if err != nil {
			errs.addErrorString("Missing required rabbit_mq_password section")
		} else {
			config.AMQPPassword = val
		}

		ival, err = config.GetInt("rabbit_mq_port")
		if err == nil {
			config.AMQPPort = ival
		}

		bval, err = config.GetBool("rabbit_mq_durable_queues")
		if err == nil {
			config.AMQPDurableQueues = bval
		}

		if len(config.AMQPUsername) == 0 || len(config.AMQPPassword) == 0 {
			config.AMQPUsername, config.AMQPPassword, err = parseCbConf()
			if err != nil {
				errs.addError(err)
			}
		}

		bval, err = config.GetBool("rabbit_mq_use_tls")
		if err == nil {
			config.AMQPTLSEnabled = bval
		}

		rabbitKeyFilename, err := config.GetString("rabbit_mq_key")
		if err == nil {
			config.AMQPTLSClientKey = rabbitKeyFilename
		}

		rabbitCertFilename, err := config.GetString("rabbit_mq_cert")
		if err == nil {
			config.AMQPTLSClientCert = rabbitCertFilename
		}

		rabbitCaCertFilename, err := config.GetString("rabbit_mq_ca_cert")
		if err == nil {
			config.AMQPTLSCACert = rabbitCaCertFilename
		}

		rabbitQueueName, err := config.GetString("rabbit_mq_queue_name")
		if err == nil {
			config.AMQPQueueName = rabbitQueueName
		}

		config.AMQPAutomaticAcking = true
		rabbitAutomaticAcking, err := config.GetBool("rabbit_mq_automatic_acking")

		if err == nil {
			config.AMQPAutomaticAcking = rabbitAutomaticAcking
		} else {
			log.Warn("Unknown value for 'rabbit_mq_automatic_acking': valid values are true, false, 1, 0. Default is 'true'")
		}

		val, err = config.GetString("cb_server_hostname")
		if err == nil {
			config.AMQPHostname = val
		}

		val, err = config.GetString("cb_server_url")
		if err == nil {
			if !strings.HasSuffix(val, "/") {
				val = val + "/"
			}
			config.CbServerURL = val
		}

		val, err = config.GetString("output_format")
		if err == nil {
			val = strings.TrimSpace(val)
			val = strings.ToLower(val)
			if val == "leef" {
				config.OutputFormat = LEEFOutputFormat
			}
			if val == "cef" {
				config.OutputFormat = CEFOutputFormat
				val, err := config.GetInt("cef_event_severity")
				if err == nil {
					config.CefEventSeverity = val
				} else {
					config.CefEventSeverity = 5
				}
			}
			if val == "template" {
				config.OutputFormat = TemplateOutputFormat
			} //config.EncoderTemplate nil when not configured
		}

		config.FileHandlerCompressData = false
		bval, err = config.GetBool("compress_data")
		if err == nil {
			config.FileHandlerCompressData = bval
		}

		config.AuditLog = false
		bval, err = config.GetBool("audit_log")
		if err == nil {
			config.AuditLog = bval
		}

		outType, err := config.GetString("output_type")
		var parameterKey string
		if err == nil {
			outType = strings.TrimSpace(outType)
			outType = strings.ToLower(outType)

			switch outType {
			case "file":
				parameterKey = "outfile"
				config.OutputType = FileOutputType
			case "tcp":
				parameterKey = "tcpout"
				config.OutputType = TCPOutputType
			case "udp":
				parameterKey = "udpout"
				config.OutputType = UDPOutputType
			case "s3":
				parameterKey = "s3out"
				config.OutputType = S3OutputType

				profileName, err := config.GetString("s3", "credential_profile")
				if err == nil {
					config.S3CredentialProfileName = profileName
				}

				aclPolicy, err := config.GetString("s3", "acl_policy")
				if err == nil {
					config.S3ACLPolicy = aclPolicy
				}

				sseType, err := config.GetString("s3", "server_side_encryption")
				if err == nil {
					config.S3ServerSideEncryption = sseType
				}

				objectPrefix, err := config.GetString("s3", "object_prefix")
				if err == nil {
					config.S3ObjectPrefix = objectPrefix
				}

			case "http":
				parameterKey = "httpout"
				config.OutputType = HTTPOutputType

				token, err := config.GetString("http", "authorization_token")
				if err == nil {
					config.HTTPAuthorizationToken = token
				}

				postTemplate, err := config.GetString("http", "http_post_template")
				config.HTTPPostTemplate = template.New("http_post_output")
				if err == nil {
					config.HTTPPostTemplate = template.Must(config.HTTPPostTemplate.Parse(postTemplate))
				} else {
					if config.OutputFormat == JSONOutputFormat {
						config.HTTPPostTemplate = template.Must(config.HTTPPostTemplate.Parse(
							`{"filename": "{{.FileName}}", "service": "carbonblack", "alerts":[{{range .Events}}{{.EventText}}{{end}}]}`))
					} else {
						config.HTTPPostTemplate = template.Must(config.HTTPPostTemplate.Parse(`{{range .Events}}{{.EventText}}{{end}}`))
					}
				}

				contentType, err := config.GetString("http", "content_type")
				if err == nil {
					config.HTTPContentType = contentType
				} else {
					jsonString := "application/json"
					config.HTTPContentType = jsonString
				}
			case "syslog":
				parameterKey = "syslogout"
				config.OutputType = SyslogOutputType
			case "splunk":
				parameterKey = "splunkout"
				config.OutputType = SplunkOutputType

				token, err := config.GetString("splunk", "hec_token")
				if err == nil {
					config.SplunkToken = &token
				}

				postTemplate, err := config.GetString("splunk", "http_post_template")
				config.HTTPPostTemplate = template.New("http_post_output")
				if err == nil {
					config.HTTPPostTemplate = template.Must(config.HTTPPostTemplate.Parse(postTemplate))
				} else {
					if config.OutputFormat == JSONOutputFormat {
						config.HTTPPostTemplate = template.Must(config.HTTPPostTemplate.Parse(
							`{{range .Events}}{"sourcetype":"bit9:carbonblack:json","event":{{.EventText}}}{{end}}`))
					} else {
						config.HTTPPostTemplate = template.Must(config.HTTPPostTemplate.Parse(`{{range .Events}}{{.EventText}}{{end}}`))
					}
				}

				contentType, err := config.GetString("http", "content_type")
				if err == nil {
					config.HTTPContentType = contentType
				} else {
					jsonString := "application/json"
					config.HTTPContentType = jsonString
				}
			case "plugin":
				config.OutputType = PluginOutputType
			default:
				errs.addErrorString(fmt.Sprintf("Unknown output type: %s", outType))
			}
		} else {
			errs.addErrorString("No output type specified")
			return globs, configs, errs
		}

		if len(parameterKey) > 0 {
			val, err = config.GetString(parameterKey)
			if err != nil {
				errs.addErrorString(fmt.Sprintf("Missing value for key %s, required by output type %s",
					parameterKey, outType))
			} else {
				config.OutputParameters = val
			}
		}

		bval, err = config.GetBool("use_raw_sensor_exchange")
		if err == nil {
			config.UseRawSensorExchange = bval
			if bval {
				log.Warn("Configured to listen on the Carbon Black Enterprise Response raw sensor event feed.")
				log.Warn("- This will result in a *large* number of messages output via the event forwarder!")
				log.Warn("- Ensure that raw sensor events are enabled in your Cb server (master & minion) via")
				log.Warn("  the 'EnableRawSensorDataBroadcast' variable in /etc/cb/cb.conf")
			}
		} else {
			log.Warn("Unknown value for 'use_raw_sensor_exchange': valid values are true, false, 1, 0")
			config.UseRawSensorExchange = false
		}

		// TLS configuration
		clientKeyFilename, err := config.GetString(outType, "client_key")
		if err == nil {
			config.TLSClientKey = clientKeyFilename
		}

		clientCertFilename, err := config.GetString(outType, "client_cert")
		if err == nil {
			config.TLSClientCert = clientCertFilename
		}

		caCertFilename, err := config.GetString(outType, "ca_cert")
		if err == nil {
			config.TLSCACert = caCertFilename
		}

		config.TLSVerify = true
		tlsVerify, err := config.GetBool(outType, "tls_verify")
		if err == nil {
			config.TLSVerify = tlsVerify
		} else {
			config.TLSVerify = true
			log.Warn("Unknown value for 'tls_verify': valid values are true, false, 1, 0. Default is 'true'")
		}

		config.TLS12Only = true
		tlsInsecure, err := config.GetBool(outType, "insecure_tls")
		if err == nil {
			config.TLS12Only = tlsInsecure
		} else {
			config.TLS12Only = true
			log.Warn("Unknown value for 'insecure_tls': ")
		}

		serverCName, err := config.GetString(outType, "server_cname")
		if err == nil {
			config.TLSCName = serverCName
		}

		config.configureTLS()

		// Bundle configuration

		// default to sending empty files to S3/HTTP POST endpoint
		if outType == "splunk" {
			config.UploadEmptyFiles = false
			log.Info("Splunk HEC does not accept empty files as config, ignoring upload_empty_files=true for 'splunkout'")
		} else {
			config.UploadEmptyFiles = true
		}
		sendEmptyFiles, err := config.GetBool(outType, "upload_empty_files")
		if err == nil {
			config.UploadEmptyFiles = sendEmptyFiles
		} else {
			config.UploadEmptyFiles = true
			log.Warn("Unknown value for 'upload_empty_files': valid values are true, false, 1, 0. Default is 'true'")
		}

		if config.OutputFormat == JSONOutputFormat {
			config.CommaSeparateEvents = true
		} else if config.OutputFormat == TemplateOutputFormat {
			commaSeparatedEvents, err := config.GetBool("encoder", "comma_seperated")
			if err == nil {
				config.CommaSeparateEvents = commaSeparatedEvents
			} else {
				config.CommaSeparateEvents = false
			}
		} else {
			config.CommaSeparateEvents = false
		}

		// default 10MB bundle size max before forcing a send
		config.BundleSizeMax = 10 * 1024 * 1024
		bundleSizeMax, err := config.GetInt(outType, "bundle_size_max")
		if err == nil {
			config.BundleSizeMax = int64(bundleSizeMax)
		}

		// default 5 minute send interval
		config.BundleSendTimeout = 5 * time.Minute
		bundleSendTimeout, err := config.GetInt(outType, "bundle_send_timeout")
		if err == nil {
			config.BundleSendTimeout = time.Duration(bundleSendTimeout) * time.Second
		}

		bval, err = config.GetBool("api_verify_ssl")
		if err == nil {
			config.CbAPIVerifySSL = bval
		} else {
			log.Warn("Unknown value for 'api_verify_ssl': valid values are true, false, 1, 0. Default is 'false'")
			config.CbAPIVerifySSL = false
		}
		val, err = config.GetString("api_token")
		if err == nil {
			config.CbAPIToken = val
			config.PerformFeedPostprocessing = true
		}

		config.CbAPIProxyURL = ""
		val, err = config.GetString("api_proxy_url")
		if err == nil {
			config.CbAPIProxyURL = val
		}

		config.parseEventTypes()

		config.PluginPath = "."

		if config.OutputType == PluginOutputType {
			log.Warn("!!!LOADING OUTPUT PLUGIN!!!")
			plug, err := config.GetString("plugin")
			if err == nil {
				config.Plugin = plug
				strPluginPath, err := config.GetString("plugin_path")
				if err == nil {
					config.PluginPath = strPluginPath
					log.Debugf("Got plugin path %s correctly", strPluginPath)

				} else {
					config.PluginPath = "."
					errs.addErrorString("Unable to parse plugin_path from config [plugin] section")
				}
			} else {
				log.Info("No plugin specified in [plugin]!")
				errs.addErrorString("Unable to parse plugin from config [plugin] section")
			}

		}


		tempconfigs[config_index] = config

	}

	if !errs.Empty {
		return globs, configs, errs
	}

	return globs, configs, nil

}*/



func GetTLSConfigFromCfg(cfg map[interface{}] interface{}) (*tls.Config,error){

	tlsConfig := &tls.Config{}

	if tlsverifyT, ok := cfg["tls_verify"]; ok {
		tlsConfig.InsecureSkipVerify = !tlsverifyT.(bool)
	} else {
		log.Warn("!!!Disabling TLS verification!!!")
		tlsConfig.InsecureSkipVerify = true
	}

	tlsClientCert := ""
	if tlsClientCertTemp,ok := cfg["tls_client_cert"]; ok {
		tlsClientCert = tlsClientCertTemp.(string)
	}
	tlsClientKey := ""
	if tlsClientKeyTemp,ok := cfg["tls_client_key"]; ok {
		tlsClientKey = tlsClientKeyTemp.(string)
	}
	tlsCACert := ""
	if tlsCACertTemp,ok := cfg["tls_ca_cert"]; ok {
		tlsCACert = tlsCACertTemp.(string)
	}
	tlsCName := ""
	if tlsCNameTemp,ok := cfg["tls_cname"]; ok {
		tlsCName = tlsCNameTemp.(string)
	}
	tls12Only := true
	if tls12OnlyTemp, ok := cfg["tls_12_only"]; ok {
		tls12Only = tls12OnlyTemp.(bool)
	}

	if tlsClientCert != "" && tlsClientKey != ""  {
		log.Infof("Loading client cert/key from %s & %s", tlsClientCert, tlsClientKey)
		cert, err := tls.LoadX509KeyPair(tlsClientCert, tlsClientKey)
		if err != nil {
			return nil,err
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	}

	if tlsCACert != "" && len(tlsCACert) > 0 {
		// Load CA cert
		log.Infof("Loading valid CAs from file %s", tlsCACert)
		caCert, err := ioutil.ReadFile(tlsCACert)
		if err != nil {
			return nil,err
		}
		caCertPool := x509.NewCertPool()
		caCertPool.AppendCertsFromPEM(caCert)
		tlsConfig.RootCAs = caCertPool
	}

	if tlsCName != "" && len(tlsCName) > 0{
		log.Infof("Forcing TLS Common Name check to use '%s' as the hostname", tlsCName)
		tlsConfig.ServerName =  tlsCName
	}

	if tls12Only == true {
		log.Info("Enforcing minimum TLS version 1.2")
		tlsConfig.MinVersion = tls.VersionTLS12
	} else {
		log.Info("Relaxing minimum TLS version to 1.0")
		tlsConfig.MinVersion = tls.VersionTLS10
	}

	return tlsConfig, nil
}
