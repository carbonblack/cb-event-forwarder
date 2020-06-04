package main

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	_ "expvar"
	"fmt"
	"io/ioutil"
	"os"
	"runtime"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/go-ini/ini"
	log "github.com/sirupsen/logrus"
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

	//DRY RUN CONTROLS REAL OUTPUT - WHEN DRYRUN IS TRUE, REAL OUTPUT WILL NOT OCCUR
	DryRun bool
	//CannedInput bool CONTROLS REAL INPUT - WHEN CANNEDINPUT IS TRUE, bundles from stress_rabbit will be used instead
	CannedInput bool

	// this is a hack for S3 specific configuration
	S3ServerSideEncryption  *string
	S3CredentialProfileName *string
	S3ACLPolicy             *string
	S3ObjectPrefix          *string
	S3Endpoint              *string
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

	CompressHTTPPayload bool

	// configuration options common to bundled outputs (S3, HTTP)
	UploadEmptyFiles    bool
	CommaSeparateEvents bool
	BundleSendTimeout   time.Duration
	BundleSizeMax       int64

	// Compress data on S3 or file output types
	FileHandlerCompressData bool
	CompressionLevel        int

	TLSConfig *tls.Config

	// optional post processing of feed hits to retrieve titles
	PerformFeedPostprocessing bool
	CbAPIToken                string
	CbAPIVerifySSL            bool
	CbAPIProxyURL             string

	// Kafka-specific configuration
	KafkaBrokers        *string
	KafkaTopicSuffix    string
	KafkaTopic          string
	KafkaProtocol       string
	KafkaMechanism      string
	KafkaUsername       string
	KafkaPassword       string
	KafkaMaxRequestSize int32

	KafkaCompressionType *string

	KafkaSSLKeyPassword *string
	KafkaSSLKeyLocation *string

	KafkaSSLCertificateLocation *string
	KafkaSSLCALocation          *string

	KafkaSSLEnabledProtocols []string

	//Splunkd
	SplunkToken *string

	RemoveFromOutput []string
	AuditLog         bool
	NumProcessors    int

	UseTimeFloat bool

	//graphite/carbon
	RunMetrics            bool
	CarbonMetricsEndpoint *string
	MetricTag             string
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

func GetLocalRabbitMQCredentials() (username, password string, err error) {
	input, err := ini.Load("/etc/cb/cb.conf")
	if err != nil {
		return "error", "error", err
	}
	username = input.Section("").Key("RabbitMQUser").Value()
	password = input.Section("").Key("RabbitMQPassword").Value()

	if len(username) == 0 || len(password) == 0 {
		return username, password, errors.New("Could not get RabbitMQ credentials from /etc/cb/cb.conf")
	}
	return username, password, nil
}

func (c *Configuration) parseEventTypes(input *ini.File) {
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
		if input.Section("bridge").HasKey(eventType.configKey) {
			key := input.Section("bridge").Key(eventType.configKey)
			val := key.Value()
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

	input, err := ini.Load(fn)
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

	if !input.Section("bridge").HasKey("server_name") {
		config.ServerName = "CB"
	} else {
		key := input.Section("bridge").Key("server_name")
		config.ServerName = key.Value()
	}

	if input.Section("bridge").HasKey("debug") {
		key := input.Section("bridge").Key("debug")
		val := key.Value()
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

	if input.Section("bridge").HasKey("remove_from_output") {
		key := input.Section("bridge").Key("remove_from_output")
		thingsToRemove := strings.Split(key.Value(), ",")
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

	if input.Section("bridge").HasKey("debug_store") {
		key := input.Section("bridge").Key("debug_store")
		config.DebugStore = key.Value()
	} else {
		config.DebugStore = "/var/log/cb/integrations/cb-event-forwarder"
	}

	log.Debugf("Debug Store is %s", config.DebugStore)

	if input.Section("bridge").HasKey("http_server_port") {
		key := input.Section("bridge").Key("http_server_port")
		port, err := strconv.Atoi(key.Value())
		if err == nil {
			config.HTTPServerPort = port
		}
	}

	if input.Section("bridge").HasKey("rabbit_mq_username") {
		key := input.Section("bridge").Key("rabbit_mq_username")
		config.AMQPUsername = key.Value()
	}

	if !input.Section("bridge").HasKey("rabbit_mq_password") {
		errs.addErrorString("Missing required rabbit_mq_password section")
	} else {
		key := input.Section("bridge").Key("rabbit_mq_password")
		config.AMQPPassword = key.Value()
	}

	if input.Section("bridge").HasKey("rabbit_mq_port") {
		key := input.Section("bridge").Key("rabbit_mq_port")
		port, err := strconv.Atoi(key.Value())
		if err == nil {
			config.AMQPPort = port
		}
	}

	if len(config.AMQPUsername) == 0 || len(config.AMQPPassword) == 0 {
		config.AMQPUsername, config.AMQPPassword, err = GetLocalRabbitMQCredentials()
		if err != nil {
			errs.addError(err)
		}
	}

	if input.Section("bridge").HasKey("rabbit_mq_use_tls") {
		key := input.Section("bridge").Key("rabbit_mq_use_tls")
		b, err := key.Bool()
		if err == nil {
			config.AMQPTLSEnabled = b
		}
	}

	if input.Section("bridge").HasKey("rabbit_mq_key") {
		key := input.Section("bridge").Key("rabbit_mq_key")
		config.AMQPTLSClientKey = key.Value()
	}

	if input.Section("bridge").HasKey("rabbit_mq_cert") {
		key := input.Section("bridge").Key("rabbit_mq_cert")
		config.AMQPTLSClientCert = key.Value()
	}

	if input.Section("bridge").HasKey("rabbit_mq_ca_cert") {
		key := input.Section("bridge").Key("rabbit_mq_ca_cert")
		config.AMQPTLSCACert = key.Value()
	}

	if input.Section("bridge").HasKey("rabbit_mq_queue_name") {
		key := input.Section("bridge").Key("rabbit_mq_queue_name")
		config.AMQPQueueName = key.Value()
	}

	config.AMQPAutomaticAcking = true

	if input.Section("bridge").HasKey("rabbit_mq_automatic_acking") {
		key := input.Section("bridge").Key("rabbit_mq_automatic_acking")
		boolval, err := key.Bool()
		if err == nil {
			if boolval == false {
				config.AMQPAutomaticAcking = false
			}
		} else {
			errs.addErrorString("Unknown value for 'rabbit_mq_automatic_acking': valid values are true, false, 1, 0. Default is 'true'")
		}
	}

	config.DryRun = false

	if input.Section("bridge").HasKey("dry_run") {
		key := input.Section("bridge").Key("dry_run")
		boolval, err := key.Bool()
		if err == nil {
			config.DryRun = boolval
		} else {
			errs.addErrorString("Unknown value for 'dry_run': valid values are true, false, 1, 0. Default is 'false'")
		}
	}

	config.CannedInput = false

	cannedInputEnvVar := os.Getenv("EF_CANNED_INPUT")

	if cannedInputEnvVar != "" {
		config.CannedInput, _ = strconv.ParseBool(cannedInputEnvVar)
	} else {
		if input.Section("bridge").HasKey("canned_input") {
			key := input.Section("bridge").Key("canned_input")
			boolval, err := key.Bool()
			if err == nil {
				config.CannedInput = boolval
			} else {
				errs.addErrorString("Unknown value for 'canned_input': valid values are true, false, 1, 0. Default is 'false'")
			}
		}
	}

	if input.Section("bridge").HasKey("cb_server_hostname") {
		key := input.Section("bridge").Key("cb_server_hostname")
		config.AMQPHostname = key.Value()
	}

	if input.Section("bridge").HasKey("cb_server_url") {
		key := input.Section("bridge").Key("cb_server_url")
		val := key.Value()
		if !strings.HasSuffix(val, "/") {
			val = val + "/"
		}
		config.CbServerURL = val
	}

	if input.Section("bridge").HasKey("output_format") {
		key := input.Section("bridge").Key("output_format")
		val := key.Value()
		val = strings.TrimSpace(val)
		val = strings.ToLower(val)
		if val == "leef" {
			config.OutputFormat = LEEFOutputFormat
		}
	}

	config.FileHandlerCompressData = false

	if input.Section("bridge").HasKey("compress_data") {
		key := input.Section("bridge").Key("compress_data")
		b, err := key.Bool()
		if err == nil {
			config.FileHandlerCompressData = b
		}
	}

	config.CompressionLevel = 1

	if input.Section("bridge").HasKey("compression_level") {
		key := input.Section("bridge").Key("compression_level")
		level, err := key.Int()
		if err == nil {
			config.CompressionLevel = level
		}
	}

	config.AuditLog = false

	if input.Section("bridge").HasKey("audit_log") {
		key := input.Section("bridge").Key("audit_log")
		b, err := key.Bool()
		if err == nil {
			config.AuditLog = b
		}
	}

	var parameterKey string

	if !input.Section("bridge").HasKey("output_type") {
		errs.addErrorString("No output type specified")
		return config, errs
	}

	key := input.Section("bridge").Key("output_type")
	outType := key.Value()
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

		if input.Section("s3").HasKey("credential_profile") {
			key := input.Section("s3").Key("credential_profile")
			profileName := key.Value()
			config.S3CredentialProfileName = &profileName
		}

		if input.Section("s3").HasKey("acl_policy") {
			key := input.Section("s3").Key("acl_policy")
			aclPolicy := key.Value()
			config.S3ACLPolicy = &aclPolicy
		}

		if input.Section("s3").HasKey("server_side_encryption") {
			key := input.Section("s3").Key("server_side_encryption")
			sseType := key.Value()
			config.S3ServerSideEncryption = &sseType
		}

		if input.Section("s3").HasKey("object_prefix") {
			key = input.Section("s3").Key("object_prefix")
			objectPrefix := key.Value()
			config.S3ObjectPrefix = &objectPrefix
		}

		//Optional S3 Endpoint configuration for s3 output to s3-compatible storage like Minio
		if input.Section("s3").HasKey("s3_endpoint") {
			key = input.Section("s3").Key("s3_endpoint")
			s3Endpoint := key.Value()
			config.S3Endpoint = &s3Endpoint
		}

		if input.Section("s3").HasKey("use_dual_stack") {
			key := input.Section("s3").Key("use_dual_stack")
			b, err := key.Bool()
			if err == nil {
				config.S3UseDualStack = b
			}
		}

	case "http":
		parameterKey = "httpout"
		config.OutputType = HTTPOutputType

		if input.Section("http").HasKey("authorization_token") {
			key := input.Section("http").Key("authorization_token")
			token := key.Value()
			config.HTTPAuthorizationToken = &token
		}

		config.HTTPPostTemplate = template.New("http_post_output")
		if input.Section("http").HasKey("http_post_template") {
			key := input.Section("http").Key("http_post_template")
			postTemplate := key.Value()
			config.HTTPPostTemplate = template.Must(config.HTTPPostTemplate.Parse(postTemplate))
		} else {
			if config.OutputFormat == JSONOutputFormat {
				config.HTTPPostTemplate = template.Must(config.HTTPPostTemplate.Parse(
					`{"filename": "{{.FileName}}", "service": "carbonblack", "alerts":[{{range .Events}}{{.EventText}}{{end}}]}`))
			} else {
				config.HTTPPostTemplate = template.Must(config.HTTPPostTemplate.Parse(`{{range .Events}}{{.EventText}}{{end}}`))
			}
		}

		if input.Section("http").HasKey("content_type") {
			key := input.Section("http").Key("content_type")
			contentType := key.Value()
			config.HTTPContentType = &contentType
		} else {
			jsonString := "application/json"
			config.HTTPContentType = &jsonString
		}

		// Parse OAuth related configuration.
		parseOAuthConfiguration(input, &config, &errs)

		if input.Section("http").HasKey("event_text_as_json_byte_array") {
			key := input.Section("http").Key("event_text_as_json_byte_array")
			boolval, err := key.Bool()
			if err == nil {
				config.EventTextAsJsonByteArray = boolval
			} else {
				errs.addErrorString(fmt.Sprintf("Invalid event_text_as_json_byte_array: %s", key.Value()))
			}
		}

		config.CompressHTTPPayload = false

		if input.Section("http").HasKey("compress_http_payload") {
			key := input.Section("http").Key("compress_http_payload")
			boolval, err := key.Bool()
			if err == nil {
				config.CompressHTTPPayload = boolval
			} else {
				errs.addErrorString(fmt.Sprintf("Invalid compress_http_payload: %s", key.Value()))
			}
		}

	case "syslog":
		parameterKey = "syslogout"
		config.OutputType = SyslogOutputType
	case "kafka":
		config.OutputType = KafkaOutputType

		if input.Section("kafka").HasKey("brokers") {
			key := input.Section("kafka").Key("brokers")
			kafkaBrokers := key.Value()
			config.KafkaBrokers = &kafkaBrokers
		}

		if input.Section("kafka").HasKey("topic_suffix") {
			key := input.Section("kafka").Key("topic_suffix")
			kafkaTopicSuffix := key.Value()
			config.KafkaTopicSuffix = kafkaTopicSuffix
		}

		if input.Section("kafka").HasKey("topic") {
			key := input.Section("kafka").Key("topic")
			kafkaTopic := key.Value()
			config.KafkaTopic = kafkaTopic
		}

		if input.Section("kafka").HasKey("protocol") {
			key := input.Section("kafka").Key("protocol")
			kafkaProtocol := key.Value()
			config.KafkaProtocol = kafkaProtocol
		}

		if input.Section("kafka").HasKey("mechanism") {
			key := input.Section("kafka").Key("mechanism")
			kafkaMechanism := key.Value()
			config.KafkaMechanism = kafkaMechanism
		}

		if input.Section("kafka").HasKey("username") {
			key := input.Section("kafka").Key("username")
			kafkaUsername := key.Value()
			config.KafkaUsername = kafkaUsername
		}

		if input.Section("kafka").HasKey("password") {
			key := input.Section("kafka").Key("password")
			kafkaPassword := key.Value()
			config.KafkaPassword = kafkaPassword
		}

		if input.Section("kafka").HasKey("max_request_size") {
			key := input.Section("kafka").Key("max_request_size")
			if intKafkaMaxRequestSize, err := key.Int(); err == nil {
				config.KafkaMaxRequestSize = int32(intKafkaMaxRequestSize)
			}
		} else {
			config.KafkaMaxRequestSize = 1000000 //sane default from issue 959 on sarama github
		}

		if input.Section("kafka").HasKey("compression_type") {
			key := input.Section("kafka").Key("compression_type")
			compressionType := key.Value()
			config.KafkaCompressionType = &compressionType
		}

		if input.Section("kafka").HasKey("ssl_ca_location") {
			key := input.Section("kafka").Key("ssl_ca_location")
			SSLCALocation := key.Value()
			config.KafkaSSLCALocation = &SSLCALocation
		}

		if input.Section("kafka").HasKey("ssl_cert_location") {
			key := input.Section("kafka").Key("ssl_cert_location")
			SSLCertLocation := key.Value()
			config.KafkaSSLCertificateLocation = &SSLCertLocation
		}

		if input.Section("kafka").HasKey("ssl_key_location") {
			key := input.Section("kafka").Key("ssl_key_location")
			SSLKeyLocation := key.Value()
			config.KafkaSSLKeyLocation = &SSLKeyLocation
		}

		if input.Section("kafka").HasKey("ssl_key_password") {
			key := input.Section("kafka").Key("ssl_key_password")
			SSLKeyPassword := key.Value()
			config.KafkaSSLKeyPassword = &SSLKeyPassword
		}

	case "splunk":
		parameterKey = "splunkout"
		config.OutputType = SplunkOutputType

		if input.Section("splunk").HasKey("hec_token") {
			key := input.Section("splunk").Key("hec_token")
			token := key.Value()
			config.SplunkToken = &token
		}

		config.HTTPPostTemplate = template.New("http_post_output")
		if input.Section("splunk").HasKey("http_post_template") {
			key := input.Section("splunk").Key("http_post_template")
			postTemplate := key.Value()
			config.HTTPPostTemplate = template.Must(config.HTTPPostTemplate.Parse(postTemplate))
		} else {
			if config.OutputFormat == JSONOutputFormat {
				config.HTTPPostTemplate = template.Must(config.HTTPPostTemplate.Parse(
					`{{range .Events}}{"sourcetype":"bit9:carbonblack:json","event":{{.EventText}}}{{end}}`))
			} else {
				config.HTTPPostTemplate = template.Must(config.HTTPPostTemplate.Parse(`{{range .Events}}{{.EventText}}{{end}}`))
			}
		}

		if input.Section("http").HasKey("content_type") {
			key := input.Section("http").Key("content_type")
			contentType := key.Value()
			config.HTTPContentType = &contentType
		} else {
			jsonString := "application/json"
			config.HTTPContentType = &jsonString
		}

	default:
		errs.addErrorString(fmt.Sprintf("Unknown output type: %s", outType))
	}

	if len(parameterKey) > 0 {
		if !input.Section("bridge").HasKey(parameterKey) {
			errs.addErrorString(fmt.Sprintf("Missing value for key %s, required by output type %s",
				parameterKey, outType))
		} else {
			key := input.Section("bridge").Key(parameterKey)
			config.OutputParameters = key.Value()
		}
	}

	if input.Section("bridge").HasKey("use_raw_sensor_exchange") {
		key := input.Section("bridge").Key("use_raw_sensor_exchange")
		boolval, err := key.Bool()
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

	if input.Section(outType).HasKey("client_key") {
		key := input.Section(outType).Key("client_key")
		clientKeyFilename := key.Value()
		config.TLSClientKey = &clientKeyFilename
	}

	if input.Section(outType).HasKey("client_cert") {
		key := input.Section(outType).Key("client_cert")
		clientCertFilename := key.Value()
		config.TLSClientCert = &clientCertFilename
	}

	if input.Section(outType).HasKey("ca_cert") {
		key := input.Section(outType).Key("ca_cert")
		caCertFilename := key.Value()
		config.TLSCACert = &caCertFilename
	}

	config.TLSVerify = true

	if input.Section(outType).HasKey("tls_verify") {
		key := input.Section(outType).Key("tls_verify")
		boolval, err := key.Bool()
		if err == nil {
			if boolval == false {
				config.TLSVerify = false
			}
		} else {
			errs.addErrorString(fmt.Sprintf("%v", err))
		}
	}

	config.TLS12Only = true

	if input.Section(outType).HasKey("insecure_tls") {
		key := input.Section(outType).Key("insecure_tls")
		boolval, err := key.Bool()
		if err == nil {
			if boolval == true {
				config.TLS12Only = false
			}
		} else {
			errs.addErrorString("Unknown value for 'insecure_tls': ")
		}
	}

	if input.Section(outType).HasKey("server_cname") {
		key := input.Section(outType).Key("server_cname")
		serverCName := key.Value()
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

	if input.Section(outType).HasKey("upload_empty_files") {
		key := input.Section(outType).Key("upload_empty_files")
		boolval, err := key.Bool()
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
	if input.Section(outType).HasKey("bundle_size_max") {
		key := input.Section(outType).Key("bundle_size_max")
		bundleSizeMax, err := key.Int64()
		if err == nil {
			config.BundleSizeMax = bundleSizeMax
		}
	}

	// default 5 minute send interval
	config.BundleSendTimeout = 5 * time.Minute

	if input.Section(outType).HasKey("bundle_send_timeout") {
		key := input.Section(outType).Key("bundle_send_timeout")
		bundleSendTimeout, err := key.Int64()
		if err == nil {
			config.BundleSendTimeout = time.Duration(bundleSendTimeout) * time.Second
		}
	}

	if input.Section("bridge").HasKey("api_verify_ssl") {
		key := input.Section("bridge").Key("api_verify_ssl")
		config.CbAPIVerifySSL, err = key.Bool()
		if err != nil {
			errs.addErrorString("Unknown value for 'api_verify_ssl': valid values are true, false, 1, 0. Default is 'false'")
		}
	}

	if input.Section("bridge").HasKey("api_token") {
		key := input.Section("bridge").Key("api_token")
		config.CbAPIToken = key.Value()
		config.PerformFeedPostprocessing = true
	}

	config.CbAPIProxyURL = ""

	if input.Section("bridge").HasKey("api_proxy_url") {
		key := input.Section("bridge").Key("api_proxy_url")
		config.CbAPIProxyURL = key.Value()
	}

	if input.Section("bridge").HasKey("message_processor_count") {
		key := input.Section("bridge").Key("message_processor_count")
		if numprocessors, err := key.Int(); err == nil {
			config.NumProcessors = int(numprocessors)
		} else {
			config.NumProcessors = runtime.NumCPU()
		}
	}

	if input.Section("bridge").HasKey("run_metrics") {
		key := input.Section("bridge").Key("run_metrics")
		if runMetrics, err := key.Bool(); err == nil {
			config.RunMetrics = runMetrics
		} else {
			config.RunMetrics = false
		}
	} else {
		config.RunMetrics = false
	}

	if input.Section("bridge").HasKey("carbon_metrics_endpoint") {
		key := input.Section("bridge").Key("carbon_metrics_endpoint")
		metricsEndpoint := key.Value()
		config.CarbonMetricsEndpoint = &metricsEndpoint
	} else {
		config.CarbonMetricsEndpoint = nil
	}

	config.MetricTag = "cb.eventforwarder"
	metricTagEnv := os.Getenv("EF_METRIC_TAG")
	if metricTagEnv != "" {
		config.MetricTag = metricTagEnv
	}

	if input.Section("bridge").HasKey("use_time_float") {
		key := input.Section("bridge").Key("use_time_float")
		usetimefloat, _ := key.Bool()
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

	if input.Section("http").HasKey("oauth_jwt_client_email") {
		key := input.Section("http").Key("oauth_jwt_client_email")
		oAuthJwtClientEmail := key.Value()
		if len(oAuthJwtClientEmail) > 0 {
			oAuthFieldsConfigured["oauth_jwt_client_email"] = true
			config.OAuthJwtClientEmail = oAuthJwtClientEmail
		} else {
			errs.addErrorString("Empty value is specified for oauth_jwt_client_email")
		}
	}

	if input.Section("http").HasKey("oauth_jwt_private_key") {
		key := input.Section("http").Key("oauth_jwt_private_key")
		oAuthJwtPrivateKey := key.Value()
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

	if input.Section("http").HasKey("oauth_jwt_private_key_id") {
		key := input.Section("http").Key("oauth_jwt_private_key_id")
		oAuthJwtPrivateKeyId := key.Value()
		if len(oAuthJwtPrivateKeyId) > 0 {
			oAuthFieldsConfigured["oauth_jwt_private_key_id"] = true
			config.OAuthJwtPrivateKeyId = oAuthJwtPrivateKeyId
		} else {
			errs.addErrorString("Empty value is specified for oauth_jwt_private_key_id")
		}
	}

	if input.Section("http").HasKey("oauth_jwt_scopes") {
		key := input.Section("http").Key("oauth_jwt_scopes")
		scopesStr := key.Value()
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

	if input.Section("http").HasKey("oauth_jwt_token_url") {
		key := input.Section("http").Key("oauth_jwt_token_url")
		oAuthJwtTokenUrl := key.Value()
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
