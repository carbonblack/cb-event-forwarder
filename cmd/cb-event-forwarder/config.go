package main

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	_ "expvar"
	"fmt"
	log "github.com/sirupsen/logrus"
	"github.com/vaughan0/go-ini"
	"io/ioutil"
	"runtime"
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
	KafkaOutputType
)

const (
	LEEFOutputFormat = iota
	JSONOutputFormat
)

type Configuration struct {
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
	S3UseDualStack          bool

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
	OAuthJwtClientEmail    string
	OAuthJwtPrivateKey     []byte
	OAuthJwtPrivateKeyId   string
	OAuthJwtScopes         []string
	OAuthJwtTokenUrl       string

	EventTextAsJsonByteArray bool

	// configuration options common to bundled outputs (S3, HTTP)
	UploadEmptyFiles    bool
	CommaSeparateEvents bool
	BundleSendTimeout   time.Duration
	BundleSizeMax       int64

	// Compress data on S3 or file output types
	FileHandlerCompressData bool

	TLSConfig *tls.Config

	// optional post processing of feed hits to retrieve titles
	PerformFeedPostprocessing bool
	CbAPIToken                string
	CbAPIVerifySSL            bool
	CbAPIProxyURL             string

	// Kafka-specific configuration
	KafkaBrokers        *string
	KafkaTopicSuffix    string
	KafkaTopic			string
	KafkaProtocol       string
	KafkaMechanism      string
	KafkaUsername       string
	KafkaPassword       string
	KafkaMaxRequestSize int32

	//Splunkd
	SplunkToken *string

	RemoveFromOutput []string
	AuditLog         bool
	NumProcessors    int

    UseTimeFloat    bool
}

type ConfigurationError struct {
	Errors []string
	Empty  bool
}

func (c *Configuration) AMQPURL() string {
	scheme := "amqp"

	if config.AMQPTLSEnabled {
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

func (c *Configuration) parseEventTypes(input ini.File) {
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

	for _, eventType := range eventTypes {
		val, ok := input.Get("bridge", eventType.configKey)
		if ok {
			val = strings.ToLower(val)
			if val == "all" {
				for _, routingKey := range eventType.eventList {
					c.EventTypes = append(c.EventTypes, routingKey)
				}
			} else if val == "0" {
				// nothing
			} else {
				for _, routingKey := range strings.Split(val, ",") {
					c.EventTypes = append(c.EventTypes, routingKey)
				}
			}
		}
	}

	c.EventMap = make(map[string]bool)

	log.Info("Raw Event Filtering Configuration:")
	for _, eventName := range c.EventTypes {
		c.EventMap[eventName] = true
		if strings.HasPrefix(eventName, "ingress.event.") {
			log.Infof("%s: %t", eventName, c.EventMap[eventName])
		}
	}
}

func ParseConfig(fn string) (Configuration, error) {
	config := Configuration{}
	errs := ConfigurationError{Empty: true}

	input, err := ini.LoadFile(fn)
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
	val, ok := input.Get("bridge", "server_name")
	if !ok {
		config.ServerName = "CB"
	} else {
		config.ServerName = val
	}

	val, ok = input.Get("bridge", "debug")
	if ok {
		if val == "1" {
			config.DebugFlag = true
			log.SetLevel(log.DebugLevel)

			customFormatter := new(log.TextFormatter)
			customFormatter.TimestampFormat = "2006-01-02 15:04:05"
			log.SetFormatter(customFormatter)
			customFormatter.FullTimestamp = true

			log.Debug("Debugging output is set to True")
		}
	}

	removeFromOutput, ok := input.Get("bridge", "remove_from_output")
	if ok {
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

	debugStore, ok := input.Get("bridge", "debug_store")
	if ok {
		config.DebugStore = debugStore
	} else {
		config.DebugStore = "/var/log/cb/integrations/cb-event-forwarder"
	}

	log.Debugf("Debug Store is %s", config.DebugStore)

	val, ok = input.Get("bridge", "http_server_port")
	if ok {
		port, err := strconv.Atoi(val)
		if err == nil {
			config.HTTPServerPort = port
		}
	}

	val, ok = input.Get("bridge", "rabbit_mq_username")
	if ok {
		config.AMQPUsername = val
	}

	val, ok = input.Get("bridge", "rabbit_mq_password")
	if !ok {
		errs.addErrorString("Missing required rabbit_mq_password section")
	} else {
		config.AMQPPassword = val
	}

	val, ok = input.Get("bridge", "rabbit_mq_port")
	if ok {
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

	val, ok = input.Get("bridge", "rabbit_mq_use_tls")
	if ok {
		if ok {
			b, err := strconv.ParseBool(val)
			if err == nil {
				config.AMQPTLSEnabled = b
			}
		}
	}

	rabbitKeyFilename, ok := input.Get("bridge", "rabbit_mq_key")
	if ok {
		config.AMQPTLSClientKey = rabbitKeyFilename
	}

	rabbitCertFilename, ok := input.Get("bridge", "rabbit_mq_cert")
	if ok {
		config.AMQPTLSClientCert = rabbitCertFilename
	}

	rabbitCaCertFilename, ok := input.Get("bridge", "rabbit_mq_ca_cert")
	if ok {
		config.AMQPTLSCACert = rabbitCaCertFilename
	}

	rabbitQueueName, ok := input.Get("bridge", "rabbit_mq_queue_name")
	if ok {
		config.AMQPQueueName = rabbitQueueName
	}

	config.AMQPAutomaticAcking = true
	rabbitAutomaticAcking, ok := input.Get("bridge", "rabbit_mq_automatic_acking")

	if ok {
		boolval, err := strconv.ParseBool(rabbitAutomaticAcking)
		if err == nil {
			if boolval == false {
				config.AMQPAutomaticAcking = false
			}
		} else {
			errs.addErrorString("Unknown value for 'rabbit_mq_automatic_acking': valid values are true, false, 1, 0. Default is 'true'")
		}
	}

	val, ok = input.Get("bridge", "cb_server_hostname")
	if ok {
		config.AMQPHostname = val
	}

	val, ok = input.Get("bridge", "cb_server_url")
	if ok {
		if !strings.HasSuffix(val, "/") {
			val = val + "/"
		}
		config.CbServerURL = val
	}

	val, ok = input.Get("bridge", "output_format")
	if ok {
		val = strings.TrimSpace(val)
		val = strings.ToLower(val)
		if val == "leef" {
			config.OutputFormat = LEEFOutputFormat
		}
	}

	config.FileHandlerCompressData = false
	val, ok = input.Get("bridge", "compress_data")
	if ok {
		b, err := strconv.ParseBool(val)
		if err == nil {
			config.FileHandlerCompressData = b
		}
	}

	config.AuditLog = false
	val, ok = input.Get("bridge", "audit_log")
	if ok {
		b, err := strconv.ParseBool(val)
		if err == nil {
			config.AuditLog = b
		}
	}

	outType, ok := input.Get("bridge", "output_type")
	var parameterKey string
	if ok {
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

			profileName, ok := input.Get("s3", "credential_profile")
			if ok {
				config.S3CredentialProfileName = &profileName
			}

			aclPolicy, ok := input.Get("s3", "acl_policy")
			if ok {
				config.S3ACLPolicy = &aclPolicy
			}

			sseType, ok := input.Get("s3", "server_side_encryption")
			if ok {
				config.S3ServerSideEncryption = &sseType
			}

			objectPrefix, ok := input.Get("s3", "object_prefix")
			if ok {
				config.S3ObjectPrefix = &objectPrefix
			}

			useDualStack, ok := input.Get("s3", "use_dual_stack")
			if ok {
				b, err := strconv.ParseBool(useDualStack)
				if err == nil {
					config.S3UseDualStack = b
				}
			}

		case "http":
			parameterKey = "httpout"
			config.OutputType = HTTPOutputType

			token, ok := input.Get("http", "authorization_token")
			if ok {
				config.HTTPAuthorizationToken = &token
			}

			postTemplate, ok := input.Get("http", "http_post_template")
			config.HTTPPostTemplate = template.New("http_post_output")
			if ok {
				config.HTTPPostTemplate = template.Must(config.HTTPPostTemplate.Parse(postTemplate))
			} else {
				if config.OutputFormat == JSONOutputFormat {
					config.HTTPPostTemplate = template.Must(config.HTTPPostTemplate.Parse(
						`{"filename": "{{.FileName}}", "service": "carbonblack", "alerts":[{{range .Events}}{{.EventText}}{{end}}]}`))
				} else {
					config.HTTPPostTemplate = template.Must(config.HTTPPostTemplate.Parse(`{{range .Events}}{{.EventText}}{{end}}`))
				}
			}

			contentType, ok := input.Get("http", "content_type")
			if ok {
				config.HTTPContentType = &contentType
			} else {
				jsonString := "application/json"
				config.HTTPContentType = &jsonString
			}

			// Parse OAuth related configuration.
			parseOAuthConfiguration(&input, &config, &errs)
			eventTextAsJsonByteArray, ok := input.Get("http", "event_text_as_json_byte_array")
			if ok {
				boolval, err := strconv.ParseBool(eventTextAsJsonByteArray)
				if err == nil {
					config.EventTextAsJsonByteArray = boolval
				} else {
					errs.addErrorString(fmt.Sprintf("Invalid event_text_as_json_byte_array: %s", eventTextAsJsonByteArray))
				}
			}

		case "syslog":
			parameterKey = "syslogout"
			config.OutputType = SyslogOutputType
		case "kafka":
			config.OutputType = KafkaOutputType

			kafkaBrokers, ok := input.Get("kafka", "brokers")
			if ok {
				config.KafkaBrokers = &kafkaBrokers
			}
			kafkaTopicSuffix, ok := input.Get("kafka", "topic_suffix")
			if ok {
				config.KafkaTopicSuffix = kafkaTopicSuffix
			}
			kafkaTopic, ok := input.Get("kafka", "topic")
			if ok {
				config.KafkaTopic = kafkaTopic
			}
			kafkaProtocol, ok := input.Get("kafka", "protocol")
			if ok {
				config.KafkaProtocol = kafkaProtocol
			}
			kafkaMechanism, ok := input.Get("kafka", "mechanism")
			if ok {
				config.KafkaMechanism = kafkaMechanism
			}
			kafkaUsername, ok := input.Get("kafka", "username")
			if ok {
				config.KafkaUsername = kafkaUsername
			}
			kafkaPassword, ok := input.Get("kafka", "password")
			if ok {
				config.KafkaPassword = kafkaPassword
			}
			kafkaMaxRequestSize, ok := input.Get("kafka", "max_request_size")
			if ok {
				if intKafkaMaxRequestSize, err := strconv.ParseInt(kafkaMaxRequestSize, 10, 32); err == nil {
					config.KafkaMaxRequestSize = int32(intKafkaMaxRequestSize)
				}
			} else {
				config.KafkaMaxRequestSize = 1000000 //sane default from issue 959 on sarama github
			}
		case "splunk":
			parameterKey = "splunkout"
			config.OutputType = SplunkOutputType

			token, ok := input.Get("splunk", "hec_token")
			if ok {
				config.SplunkToken = &token
			}

			postTemplate, ok := input.Get("splunk", "http_post_template")
			config.HTTPPostTemplate = template.New("http_post_output")
			if ok {
				config.HTTPPostTemplate = template.Must(config.HTTPPostTemplate.Parse(postTemplate))
			} else {
				if config.OutputFormat == JSONOutputFormat {
					config.HTTPPostTemplate = template.Must(config.HTTPPostTemplate.Parse(
						`{{range .Events}}{"sourcetype":"bit9:carbonblack:json","event":{{.EventText}}}{{end}}`))
				} else {
					config.HTTPPostTemplate = template.Must(config.HTTPPostTemplate.Parse(`{{range .Events}}{{.EventText}}{{end}}`))
				}
			}

			contentType, ok := input.Get("http", "content_type")
			if ok {
				config.HTTPContentType = &contentType
			} else {
				jsonString := "application/json"
				config.HTTPContentType = &jsonString
			}

		default:
			errs.addErrorString(fmt.Sprintf("Unknown output type: %s", outType))
		}
	} else {
		errs.addErrorString("No output type specified")
		return config, errs
	}

	if len(parameterKey) > 0 {
		val, ok = input.Get("bridge", parameterKey)
		if !ok {
			errs.addErrorString(fmt.Sprintf("Missing value for key %s, required by output type %s",
				parameterKey, outType))
		} else {
			config.OutputParameters = val
		}
	}

	val, ok = input.Get("bridge", "use_raw_sensor_exchange")
	if ok {
		boolval, err := strconv.ParseBool(val)
		if err == nil {
			config.UseRawSensorExchange = boolval
			if boolval {
				log.Warn("Configured to listen on the Carbon Black Enterprise Response raw sensor event feed.")
				log.Warn("- This will result in a *large* number of messages output via the event forwarder!")
				log.Warn("- Ensure that raw sensor events are enabled in your Cb server (master & minion) via")
				log.Warn("  the 'EnableRawSensorDataBroadcast' variable in /etc/cb/cb.conf")
			}
		} else {
			errs.addErrorString("Unknown value for 'use_raw_sensor_exchange': valid values are true, false, 1, 0")
		}
	}

	// TLS configuration
	clientKeyFilename, ok := input.Get(outType, "client_key")
	if ok {
		config.TLSClientKey = &clientKeyFilename
	}

	clientCertFilename, ok := input.Get(outType, "client_cert")
	if ok {
		config.TLSClientCert = &clientCertFilename
	}

	caCertFilename, ok := input.Get(outType, "ca_cert")
	if ok {
		config.TLSCACert = &caCertFilename
	}

	config.TLSVerify = true
	tlsVerify, ok := input.Get(outType, "tls_verify")
	if ok {
		boolval, err := strconv.ParseBool(tlsVerify)
		if err == nil {
			if boolval == false {
				config.TLSVerify = false
			}
		} else {
			errs.addErrorString("Unknown value for 'tls_verify': valid values are true, false, 1, 0. Default is 'true'")
		}
	}

	config.TLS12Only = true
	tlsInsecure, ok := input.Get(outType, "insecure_tls")
	if ok {
		boolval, err := strconv.ParseBool(tlsInsecure)
		if err == nil {
			if boolval == true {
				config.TLS12Only = false
			}
		} else {
			errs.addErrorString("Unknown value for 'insecure_tls': ")
		}
	}

	serverCName, ok := input.Get(outType, "server_cname")
	if ok {
		config.TLSCName = &serverCName
	}

	config.TLSConfig = configureTLS(config)

	// Bundle configuration

	// default to sending empty files to S3/HTTP POST endpoint
	if outType == "splunk" {
		config.UploadEmptyFiles = false
		log.Info("Splunk HEC does not accept empty files as input, ignoring upload_empty_files=true for 'splunkout'")
	} else {
		config.UploadEmptyFiles = true
	}
	sendEmptyFiles, ok := input.Get(outType, "upload_empty_files")
	if ok {
		boolval, err := strconv.ParseBool(sendEmptyFiles)
		if err == nil {
			if boolval == false {
				config.UploadEmptyFiles = false
			}
		} else {
			errs.addErrorString("Unknown value for 'upload_empty_files': valid values are true, false, 1, 0. Default is 'true'")
		}
	}

	if config.OutputFormat == JSONOutputFormat {
		config.CommaSeparateEvents = true
	} else {
		config.CommaSeparateEvents = false
	}

	// default 10MB bundle size max before forcing a send
	config.BundleSizeMax = 10 * 1024 * 1024
	bundleSizeMax, ok := input.Get(outType, "bundle_size_max")
	if ok {
		bundleSizeMax, err := strconv.ParseInt(bundleSizeMax, 10, 64)
		if err == nil {
			config.BundleSizeMax = bundleSizeMax
		}
	}

	// default 5 minute send interval
	config.BundleSendTimeout = 5 * time.Minute
	bundleSendTimeout, ok := input.Get(outType, "bundle_send_timeout")
	if ok {
		bundleSendTimeout, err := strconv.ParseInt(bundleSendTimeout, 10, 64)
		if err == nil {
			config.BundleSendTimeout = time.Duration(bundleSendTimeout) * time.Second
		}
	}

	val, ok = input.Get("bridge", "api_verify_ssl")
	if ok {
		config.CbAPIVerifySSL, err = strconv.ParseBool(val)
		if err != nil {
			errs.addErrorString("Unknown value for 'api_verify_ssl': valid values are true, false, 1, 0. Default is 'false'")
		}
	}
	val, ok = input.Get("bridge", "api_token")
	if ok {
		config.CbAPIToken = val
		config.PerformFeedPostprocessing = true
	}

	config.CbAPIProxyURL = ""
	val, ok = input.Get("bridge", "api_proxy_url")
	if ok {
		config.CbAPIProxyURL = val
	}

	val, ok = input.Get("bridge", "message_processor_count")
	if ok {
		if numprocessors, err := strconv.ParseInt(val, 10, 32); err == nil {
			config.NumProcessors = int(numprocessors)
		} else {
			config.NumProcessors = runtime.NumCPU() * 2
		}
	}

    val, ok = input.Get("bridge","use_time_float")
    if ok {
        usetimefloat,_ := strconv.ParseBool(val)
        config.UseTimeFloat = usetimefloat
    } else { 
       config.UseTimeFloat = false 
    }

	config.parseEventTypes(input)

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

// parseOAuthConfiguration parses OAuth related configuration from input and populates config with
// relevant fields.
func parseOAuthConfiguration(input *ini.File, config *Configuration, errs *ConfigurationError) {
	// oAuthFieldsConfigured is used to track OAuth fields that have been configured.
	oAuthFieldsConfigured := make(map[string]bool)

	if oAuthJwtClientEmail, ok := input.Get("http", "oauth_jwt_client_email"); ok {
		if len(oAuthJwtClientEmail) > 0 {
			oAuthFieldsConfigured["oauth_jwt_client_email"] = true
			config.OAuthJwtClientEmail = oAuthJwtClientEmail
		} else {
			errs.addErrorString("Empty value is specified for oauth_jwt_client_email")
		}
	}

	if oAuthJwtPrivateKey, ok := input.Get("http", "oauth_jwt_private_key"); ok {
		if len(oAuthJwtPrivateKey) > 0 {
			oAuthFieldsConfigured["oauth_jwt_private_key"] = true
			// Replace the escaped version of a line break character with the non-escaped version.
			// The OAuth library which reads private key expects non-escaped version of line break
			// character.
			oAuthJwtPrivateKey = strings.Replace(oAuthJwtPrivateKey, "\\n", "\n", -1)
			config.OAuthJwtPrivateKey = []byte(oAuthJwtPrivateKey)
		} else {
			errs.addErrorString("Empty value is specified for oauth_jwt_private_key")
		}
	}

	if oAuthJwtPrivateKeyId, ok := input.Get("http", "oauth_jwt_private_key_id"); ok {
		if len(oAuthJwtPrivateKeyId) > 0 {
			oAuthFieldsConfigured["oauth_jwt_private_key_id"] = true
			config.OAuthJwtPrivateKeyId = oAuthJwtPrivateKeyId
		} else {
			errs.addErrorString("Empty value is specified for oauth_jwt_private_key_id")
		}
	}

	if scopesStr, ok := input.Get("http", "oauth_jwt_scopes"); ok {
		if len(scopesStr) > 0 {
			oAuthFieldsConfigured["oauth_jwt_scopes"] = true

			var oAuthJwtScopes []string
			for _, scope := range strings.Split(scopesStr, ",") {
				scope = strings.TrimSpace(scope)
				if len(scope) > 0 {
					oAuthJwtScopes = append(oAuthJwtScopes, scope)
				} else {
					errs.addErrorString("Empty scope found")
				}
			}
			config.OAuthJwtScopes = oAuthJwtScopes
		} else {
			errs.addErrorString("Empty value is specified for oauth_jwt_scopes")
		}

	}

	if oAuthJwtTokenUrl, ok := input.Get("http", "oauth_jwt_token_url"); ok {
		if len(oAuthJwtTokenUrl) > 0 {
			oAuthFieldsConfigured["oauth_jwt_token_url"] = true
			config.OAuthJwtTokenUrl = oAuthJwtTokenUrl
		} else {
			errs.addErrorString("Empty value is specified for oauth_jwt_token_url")
		}
	}

	// requiredOAuthFields contains the fields that must be present if OAuth is configured.
	requiredOAuthFields := []string{
		"oauth_jwt_client_email",
		"oauth_jwt_private_key",
		"oauth_jwt_token_url",
	}

	// Check that all required fields present if OAuth is configured.
	if len(oAuthFieldsConfigured) > 0 {
		for _, requiredField := range requiredOAuthFields {
			if !oAuthFieldsConfigured[requiredField] {
				errs.addErrorString(
					fmt.Sprintf("Required OAuth field %s is not configured", requiredField))
			}
		}
	}
}
