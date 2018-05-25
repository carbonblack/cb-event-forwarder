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
	"path"
	"plugin"
	"strconv"
	"strings"
	"text/template"
	"time"
)

const (
	FileOutputType = iota
	S3OutputType
	TCPOutputType
	UDPOutputType
	SyslogOutputType
	HTTPOutputType
	SplunkOutputType
	PluginOutputType
)

const (
	LEEFOutputFormat = iota
	JSONOutputFormat
	CEFOutputFormat
	TemplateOutputFormat
)

type Configuration struct {
	ConfigMap            map[string]interface{}
	ServerName           string
	AMQPHostname         string
	DebugFlag            bool
	DebugStore           string
	OutputType           int
	OutputFormat         int
	AMQPUsername         string
	AMQPPassword         string
	AMQPPort             int
	AMQPTLSEnabled       bool
	AMQPTLSClientKey     string
	AMQPTLSClientCert    string
	AMQPTLSCACert        string
	AMQPQueueName        string
	AMQPAutomaticAcking  bool
	OutputParameters     string
	EventTypes           []string
	EventMap             map[string]bool
	HTTPServerPort       int
	CbServerURL          string
	UseRawSensorExchange bool

	// this is a hack for S3 specific configuration
	S3ServerSideEncryption  *string
	S3CredentialProfileName *string
	S3ACLPolicy             *string
	S3ObjectPrefix          *string

	// SSL/TLS-specific configuration
	TLSClientKey  *string
	TLSClientCert *string
	TLSCACert     *string
	TLSVerify     bool
	TLSCName      *string
	TLS12Only     bool

	// HTTP-specific configuration
	HTTPAuthorizationToken *string
	HTTPPostTemplate       *template.Template
	HTTPContentType        *string

	// configuration options common to bundled outputs (S3, HTTP)
	UploadEmptyFiles    bool
	CommaSeparateEvents bool
	BundleSendTimeout   time.Duration
	BundleSizeMax       int64

	//CEF output Options
	CefEventSeverity int

	// Compress data on S3 or file output types
	FileHandlerCompressData bool

	TLSConfig *tls.Config

	// optional post processing of feed hits to retrieve titles
	PerformFeedPostprocessing bool
	CbAPIToken                string
	CbAPIVerifySSL            bool
	CbAPIProxyURL             string

	//Splunkd
	SplunkToken *string

	AddToOutput      map[string]string
	RemoveFromOutput []string
	AuditLog         bool

	//Hack: Plugins
	PluginPath string
	Plugin     string

	//TemplateEncoder support
	EncoderTemplate *template.Template

	//FilterTemplate support
	FilterTemplate *template.Template
	FilterEnabled  bool
}

func loadFuncMapFromPlugin(pluginPath string, pluginName string) template.FuncMap {
	log.Infof("loadPluginFuncMap: Trying to load plugin funcmap provider %s at %s", pluginName, pluginPath)
	plug, err := plugin.Open(path.Join(pluginPath, pluginName+".so"))
	if err != nil {
		log.Panic(err)
	}
	pluginGetFuncMapRaw, err := plug.Lookup("GetFuncMap")
	if err != nil {
		log.Panicf("Failed to load encoder plugin %v", err)
	}
	return pluginGetFuncMapRaw.(func() template.FuncMap)()
}

func (c *Configuration) getByArray(lookup []string) (interface{}, error) {
	var temp interface{}
	log.Debugf("Lookup %s", lookup)
	for index, key := range lookup {
		if index == 0 {
			iface, ok := c.ConfigMap[key]
			if !ok {
				errStr := fmt.Sprintf("Couldn't find %s of %s in %s", key, lookup, c.ConfigMap)
				log.Debugf(errStr)
				return nil, errors.New(errStr)
			} else {
				log.Debugf("Found key %s of %s in %s value is %s", key, lookup, c.ConfigMap, iface)
				temp = iface
			}
		} else {
			if temp != nil {
				tempmap, ok := temp.(map[interface{}]interface{})
				if ok {
					iface, ok := tempmap[key]
					if !ok {
						errStr := fmt.Sprintf("Couldn't find %s in %s in %s within %s", key, lookup, tempmap, c.ConfigMap)
						log.Debugf(errStr)
						return iface, errors.New(errStr)
					} else {
						log.Debugf("Found key %s of %s in %s within %s value is %s", key, lookup, temp, iface, c.ConfigMap)
						temp = iface
					}
				} else {
					errStr := "Type coercion failed"
					switch t := temp.(type) {
					default:
						errStr = fmt.Sprintf("Failed to coerce temporary iface %s into map[interface{}] interface{} %T", temp, t)
					}

					log.Debugf(errStr)
					return nil, errors.New(errStr)

				}
			} else {
				errStr := fmt.Sprintf("Couldn't find %s of %s in %s within %s [TEMP IFACE IS NIL]", key, lookup, temp, c.ConfigMap)
				log.Debugf(errStr)
				return nil, errors.New(errStr)
			}
		}
	}
	log.Debugf("Lookup returning %s for %s", temp, lookup)
	return temp, nil
}

func (c * Configuration) Get(lookup ... string) (interface {} , error){
	return c.getByArray(lookup)
}

func (c * Configuration) GetWithDefault(d interface{}, lookup ... string) interface{} {
	found, err := c.getByArray(lookup)
	if err == nil {
		return found
	} else {
		return d
	}

}

func (c *Configuration) GetBool(lookup ...string) (bool, error) {
	iFaceValue, err := c.getByArray(lookup)
	if err != nil {
		return false, err

	} else {
		bval, ok := iFaceValue.(bool)
		if ok {
			return bval, nil
		} else {
			return false, errors.New(fmt.Sprintf("Can't convert to bool : %s", iFaceValue))
		}
	}
}

func (c *Configuration) GetBoolWithDefault(def bool, lookup ...string) bool {
	iFaceValue, err := c.getByArray(lookup)
	if err != nil {
		return def

	} else {
		bval, ok := iFaceValue.(bool)
		if ok {
			return bval
		} else {
			return def
		}
	}
}

func (c *Configuration) GetInt(lookup ...string) (int, error) {
	iFaceValue, err := c.getByArray(lookup)
	if err != nil {
		return 0, err

	} else {
		ival, ok := iFaceValue.(int)
		if ok {
			return ival, nil
		} else {
			return 0, errors.New(fmt.Sprintf("Can't convert to bool %s", iFaceValue))
		}
	}
}

func (c *Configuration) GetString(lookup ...string) (string, error) {
	iFaceValue, err := c.getByArray(lookup)
	if err != nil {
		return "", err
	} else {
		stringVal := ""
		switch iFaceValue.(type) {
		case string:
			var ok bool = false
			stringVal, ok = iFaceValue.(string)
			if !ok {
				return "", errors.New(fmt.Sprintf("Can't convert %s to string", iFaceValue))
			}
		case int:
			stringVal = fmt.Sprintf("%d", iFaceValue)
		case float32, float64:
			stringVal = fmt.Sprintf("%f", iFaceValue)
		case bool:
			stringVal = fmt.Sprintf("%b", iFaceValue)
		default:
			stringVal = fmt.Sprintf("%s", iFaceValue)
		}
		return stringVal, nil

	}
}

func (c *Configuration) GetStrings(lookup ...string) ([]string, error) {
	iFaceValue, err := c.getByArray(lookup)
	if err != nil {
		return make([]string, 0), err
	} else {
		stringArray, ok := iFaceValue.([]string)
		if ok {
			return stringArray, nil
		} else {
			return make([]string, 0), errors.New(fmt.Sprintf("Can't convert %s to [] string", iFaceValue))
		}
	}
}

func (c *Configuration) GetMap(lookup ...string) (map[string]interface{}, error) {
	iFaceValue, err := c.getByArray(lookup)
	var temp_map map[string]interface{}
	if err != nil {
		return temp_map, err
	} else {
		stringArray, ok := iFaceValue.(map[string]interface{})
		if ok {
			return stringArray, nil
		} else {
			return temp_map, errors.New(fmt.Sprintf("Can't convert %s to map[string] interface{}", iFaceValue))
		}
	}
}

func LoadFile(filename string) (Configuration, error) {
	var temp_conf Configuration = Configuration{}
	content, err := ioutil.ReadFile(filename)
	if err != nil {
		return temp_conf, err
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
		return temp_conf, err
	} else {
		temp_conf = Configuration{ConfigMap: m}
		return temp_conf, nil
	}
	return temp_conf, err
}

type ConfigurationError struct {
	Errors []string
	Empty  bool
}

func (c *Configuration) AMQPURL() string {
	scheme := "amqp"
	if c.AMQPTLSEnabled {
		scheme = "amqps"
	}
	return fmt.Sprintf("%s://%s:%s@%s:%d", scheme, c.AMQPUsername, c.AMQPPassword, c.AMQPHostname, c.AMQPPort)
}

func (e ConfigurationError) Error() string {
	return fmt.Sprintf("Configuration errors:\n %s", strings.Join(e.Errors, "\n "))
}

func (e *ConfigurationError) addErrorString(err string) {
	e.Empty = false
	e.Errors = append(e.Errors, err)
}

func (e *ConfigurationError) addError(err error) {
	e.Empty = false
	e.Errors = append(e.Errors, err.Error())
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

func (c *Configuration) parseEventTypes(input Configuration) {
	eventTypes := [...]struct {
		configKey string
		eventList []string
	}{
		{"events_watchlist", []string{
			"watchlist.#",
		}},
		{"events_feed", []string{
			"feed.#",
		}},
		{"events_alert", []string{
			"alert.#",
		}},
		{"events_raw_sensor", []string{
			"ingress.event.process",
			"ingress.event.procstart",
			"ingress.event.netconn",
			"ingress.event.procend",
			"ingress.event.childproc",
			"ingress.event.moduleload",
			"ingress.event.module",
			"ingress.event.filemod",
			"ingress.event.regmod",
			"ingress.event.tamper",
			"ingress.event.crossprocopen",
			"ingress.event.remotethread",
			"ingress.event.processblock",
			"ingress.event.emetmitigation",
		}},
		{"events_binary_observed", []string{
			"binaryinfo.#",
		}},
		{"events_binary_upload", []string{
			"binarystore.#",
		}},
		{"events_storage_partition", []string{
			"events.partition.#",
		}},
	}

	var eventTypesArray []string = make([]string, 24)

	for _, eventType := range eventTypes {
		val, err := input.GetString(eventType.configKey)
		if err == nil {
			val = strings.ToLower(val)
			if val == "all" {
				for _, routingKey := range eventType.eventList {
					if len(routingKey) > 0 {
						eventTypesArray = append(eventTypesArray, routingKey)
					}
				}
			} else if val == "0" {
				// nothing

			} else {
				for _, routingKey := range strings.Split(val, ",") {
					if len(routingKey) > 0 {
						eventTypesArray = append(eventTypesArray, routingKey)
					}
				}
			}
		} else {
			log.Debugf("Error processing event types in config file %v", err)
		}
	}

	var eventMap map[string]bool = make(map[string]bool)

	log.Info("Raw Event Filtering Configuration:")
	for _, eventName := range eventTypesArray {
		eventMap[eventName] = true
		if strings.HasPrefix(eventName, "ingress.event.") {
			log.Infof("%s: %t", eventName, eventMap[eventName])
		}
	}

	c.EventTypes = eventTypesArray
	c.EventMap = eventMap
}

func ParseConfig(fn string) (Configuration, error) {
	errs := ConfigurationError{Empty: true}

	config, err := LoadFile(fn)
	if err != nil {
		return config, err
	}

	// defaults
	config.DebugFlag = false
	config.OutputFormat = JSONOutputFormat
	config.OutputType = FileOutputType
	config.AMQPHostname = "localhost"
	config.AMQPUsername = "cb"
	config.HTTPServerPort = 33706
	config.AMQPPort = 5004
	config.DebugStore = "/tmp"

	config.S3ACLPolicy = nil
	config.S3ServerSideEncryption = nil
	config.S3CredentialProfileName = nil

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

	val, err = config.GetString("http_server_port")
	if err == nil {
		port, err := strconv.Atoi(val)
		if err == nil {
			config.HTTPServerPort = port
		}
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

	val, err = config.GetString("rabbit_mq_port")
	if err == nil {
		port, err := strconv.Atoi(val)
		if err == nil {
			config.AMQPPort = port
		}
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

	EncoderPlugin := ""

	val, err = config.GetString("output_format")
	if err == nil {
		val = strings.TrimSpace(val)
		val = strings.ToLower(val)
		if val == "leef" {
			config.OutputFormat = LEEFOutputFormat
		}
		if val == "cef" {
			config.OutputFormat = CEFOutputFormat
			val, err := config.GetString("cef_event_severity")
			if err == nil {
				CefEventSeverity, err := strconv.ParseInt(val, 10, 32)
				if err == nil {
					config.CefEventSeverity = 5
				} else {
					config.CefEventSeverity = int(CefEventSeverity)
				}
			}
		}
		if val == "template" {
			config.OutputFormat = TemplateOutputFormat
			val, err := config.GetString("encoder", "template")
			if err == nil {
				tmpl, err := template.New("TemplateEncoder").Parse(val)
				if err != nil {
					log.Panicf("Error setting up template for encoder", err)
				} else {
					config.EncoderTemplate = tmpl
				}
			}
			val, err = config.GetString("encoder", "plugin")
			if err == nil && len(val) > 0 {
				EncoderPlugin = val
			} else {
				EncoderPlugin = ""
			}
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
				config.S3CredentialProfileName = &profileName
			}

			aclPolicy, err := config.GetString("s3", "acl_policy")
			if err == nil {
				config.S3ACLPolicy = &aclPolicy
			}

			sseType, err := config.GetString("s3", "server_side_encryption")
			if err == nil {
				config.S3ServerSideEncryption = &sseType
			}

			objectPrefix, err := config.GetString("s3", "object_prefix")
			if err == nil {
				config.S3ObjectPrefix = &objectPrefix
			}

		case "http":
			parameterKey = "httpout"
			config.OutputType = HTTPOutputType

			token, err := config.GetString("http", "authorization_token")
			if err == nil {
				config.HTTPAuthorizationToken = &token
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
				config.HTTPContentType = &contentType
			} else {
				jsonString := "application/json"
				config.HTTPContentType = &jsonString
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
				config.HTTPContentType = &contentType
			} else {
				jsonString := "application/json"
				config.HTTPContentType = &jsonString
			}
		case "plugin":
			config.OutputType = PluginOutputType
		default:
			errs.addErrorString(fmt.Sprintf("Unknown output type: %s", outType))
		}
	} else {
		errs.addErrorString("No output type specified")
		return config, errs
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
		config.TLSClientKey = &clientKeyFilename
	}

	clientCertFilename, err := config.GetString(outType, "client_cert")
	if err == nil {
		config.TLSClientCert = &clientCertFilename
	}

	caCertFilename, err := config.GetString(outType, "ca_cert")
	if err == nil {
		config.TLSCACert = &caCertFilename
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
		config.TLSCName = &serverCName
	}

	config.TLSConfig = configureTLS(config)

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
	} else {
		config.CommaSeparateEvents = false
	}

	// default 10MB bundle size max before forcing a send
	config.BundleSizeMax = 10 * 1024 * 1024
	bundleSizeMax, err := config.GetString(outType, "bundle_size_max")
	if err == nil {
		bundleSizeMax, err := strconv.ParseInt(bundleSizeMax, 10, 64)
		if err == nil {
			config.BundleSizeMax = bundleSizeMax
		}
	}

	// default 5 minute send interval
	config.BundleSendTimeout = 5 * time.Minute
	bundleSendTimeout, err := config.GetString(outType, "bundle_send_timeout")
	if err == nil {
		bundleSendTimeout, err := strconv.ParseInt(bundleSendTimeout, 10, 64)
		if err == nil {
			config.BundleSendTimeout = time.Duration(bundleSendTimeout) * time.Second
		}
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

	config.parseEventTypes(config)

	config.PluginPath = "."

	if config.OutputType == PluginOutputType {
		log.Warn("!!!LOADING OUTPUT PLUGIN!!!")
		plugin, err := config.GetString("plugin")
		if err == nil {
			config.Plugin = plugin
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

	//load encoder plugin if specified
	if config.OutputFormat == TemplateOutputFormat && EncoderPlugin != "" {
		log.Warn("!!!LOADING ENCODER PLUGIN!!!")
		config.EncoderTemplate = config.EncoderTemplate.Funcs(loadFuncMapFromPlugin(config.PluginPath, EncoderPlugin))
	}

	filter_events := config.GetBoolWithDefault(false, "filter", "enabled")
	config.FilterEnabled = filter_events
	if config.FilterEnabled {
		log.Warn("!!!EVENT FILTERING ENALBED!!!")
		filter_temp, err := config.GetString("filter", "template")
		if err != nil {
			errs.addErrorString("Filter enabled but no filter.template specified")
		}
		config.FilterTemplate, err = template.New("eventfilter").Parse(filter_temp)
		if err != nil {
			errs.addError(err)
		}
		filter_plugin, err := config.GetString("filter","plugin")
		if err != nil {
			log.Warn("!!!NO EVENT FILTERING PLUGIN LOADED!!!")
		} else {
			log.Warn("!!!EVENT FILTERING PLUGIN ENALBED!!!")
			config.FilterTemplate = config.FilterTemplate.Funcs(loadFuncMapFromPlugin(config.PluginPath, filter_plugin))
		}
	}

	if !errs.Empty {
		return config, errs
	}

	return config, nil
}

func configureTLS(config Configuration) *tls.Config {
	tlsConfig := &tls.Config{}

	if config.TLSVerify == false {
		log.Info("Disabling TLS verification for remote output")
		tlsConfig.InsecureSkipVerify = true
	}

	if config.TLSClientCert != nil && config.TLSClientKey != nil && len(*config.TLSClientCert) > 0 &&
		len(*config.TLSClientKey) > 0 {
		log.Infof("Loading client cert/key from %s & %s", *config.TLSClientCert, *config.TLSClientKey)
		cert, err := tls.LoadX509KeyPair(*config.TLSClientCert, *config.TLSClientKey)
		if err != nil {
			log.Fatal(err)
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	}

	if config.TLSCACert != nil && len(*config.TLSCACert) > 0 {
		// Load CA cert
		log.Infof("Loading valid CAs from file %s", *config.TLSCACert)
		caCert, err := ioutil.ReadFile(*config.TLSCACert)
		if err != nil {
			log.Fatal(err)
		}
		caCertPool := x509.NewCertPool()
		caCertPool.AppendCertsFromPEM(caCert)
		tlsConfig.RootCAs = caCertPool
	}

	if config.TLSCName != nil && len(*config.TLSCName) > 0 {
		log.Infof("Forcing TLS Common Name check to use '%s' as the hostname", *config.TLSCName)
		tlsConfig.ServerName = *config.TLSCName
	}

	if config.TLS12Only == true {
		log.Info("Enforcing minimum TLS version 1.2")
		tlsConfig.MinVersion = tls.VersionTLS12
	} else {
		log.Info("Relaxing minimum TLS version to 1.0")
		tlsConfig.MinVersion = tls.VersionTLS10
	}

	return tlsConfig
}
