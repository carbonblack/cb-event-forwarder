package config

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	_ "expvar"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/go-ini/ini"
	log "github.com/sirupsen/logrus"
	"github.com/Showmax/go-fqdn"
)

const (
	FileOutputType = iota
	S3OutputType
	OLDS3OutputType
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

const DEFAULTEXITTIMEOUT = 7
const DEFAULTLOGLOCATION = "/var/log/cb/integrations/cb-event-forwarder"
const DEFAULTLOGSIZE = 50
const MAXLOGSIZE = 500
const DEFAULTLOGRETAINDAYS = 21
const MAXLOGBACKUPS = 30
const DEFAULTLOGBACKUPS = 7

func Max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func Min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

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

	// DRY RUN CONTROLS REAL OUTPUT - WHEN DRYRUN IS TRUE, REAL OUTPUT WILL NOT OCCUR
	DryRun bool
	// CannedInput bool CONTROLS REAL INPUT - WHEN CANNEDINPUT IS TRUE, bundles from stress_rabbit will be used instead
	CannedInput         bool
	CannedInputLocation *string
	// RunConsumer bool
	// Controls whether or not a rabbitmq consumer is bound
	RunConsumer bool

	// this is a hack for S3 specific configuration
	S3ServerSideEncryption  *string
	S3CredentialProfileName *string
	S3ACLPolicy             *string
	S3ObjectPrefix          *string
	S3Endpoint              *string
	S3UseDualStack          bool
	S3Concurrency           int

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
	CompressionType         CompressionType

	TLSConfig *tls.Config

	// Kafka-specific configuration
	KafkaBrokers        *string
	KafkaTopicSuffix    string
	KafkaTopic          string
	KafkaUsername       string
	KafkaPassword       string
	KafkaMaxRequestSize int32

	KafkaCompressionType *string

	KafkaSSLKeyLocation *string

	KafkaSSLCertificateLocation *string
	KafkaSSLCALocation          *string

	// Splunkd
	SplunkToken *string

	RemoveFromOutput []string
	AuditLog         bool
	NumProcessors    int

	UseTimeFloat       bool
	ExitTimeoutSeconds time.Duration

	// graphite/carbon
	RunMetrics            bool
	CarbonMetricsEndpoint *string
	MetricTag             string

	//Logging
	LogLevel   log.Level
	LogDir     string
	LogSizeMB  int
	LogBackups int
	LogMaxAge  int
}

type ConfigurationError struct {
	Errors []string
	Empty  bool
}

func (config *Configuration) AMQPURL() string {
	scheme := "amqp"

	if config.AMQPTLSEnabled {
		scheme = "amqps"
	}

	return fmt.Sprintf("%s://%s:%s@%s:%d", scheme, config.AMQPUsername, config.AMQPPassword, config.AMQPHostname, config.AMQPPort)
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

func (config *Configuration) parseEventTypes(input *ini.File) {
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
			"ingress.event.filelessscriptload",
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
		{"task_errors", []string{
			"task.error.logged",
		}},
	}

	for _, eventType := range eventTypes {
		if input.Section("bridge").HasKey(eventType.configKey) {
			key := input.Section("bridge").Key(eventType.configKey)
			val := key.Value()
			val = strings.ToLower(val)
			switch val {
			case "all":
				for _, routingKey := range eventType.eventList {
					config.EventTypes = append(config.EventTypes, routingKey)
				}
			case "0":
			default:
				for _, routingKey := range strings.Split(val, ",") {
					config.EventTypes = append(config.EventTypes, routingKey)
				}
			}
		}
	}

	config.EventMap = make(map[string]bool)

	log.Info("Raw Event Filtering Configuration:")
	for _, eventName := range config.EventTypes {
		config.EventMap[eventName] = true
		if strings.HasPrefix(eventName, "ingress.event.") {
			log.Infof("%s: %t", eventName, config.EventMap[eventName])
		}
	}
}

func (config * Configuration) parseServerName(input * ini.File) error {
	shouldSetServerNameToFqdn := false

	if !input.Section("bridge").HasKey("server_name") {
		config.ServerName = "CB"
		shouldSetServerNameToFqdn = true
	} else {
		key := input.Section("bridge").Key("server_name")
		config.ServerName = key.Value()
		if config.ServerName == "cbresponse" {
			shouldSetServerNameToFqdn = true
		}
	}

	if shouldSetServerNameToFqdn {
		fqdn, err := fqdn.FqdnHostname()
		if err != nil {
			log.Errorf("Error getting fqdn for host: %s", err)
		} else {
			log.Infof("Automatically set server_name to %s", fqdn)
			config.ServerName = fqdn
		}
	}
	return nil
}

func (config * Configuration) parseCbServerURL(input * ini.File) error {
	foundCbServerUrl := false
	if input.Section("bridge").HasKey("cb_server_url") {
		key := input.Section("bridge").Key("cb_server_url")
		val := key.Value()
		if len(val) > 0 {
			if !strings.HasSuffix(val, "/") {
				val += "/"
			}
			config.CbServerURL = val
			foundCbServerUrl = true
		}
	}
	if !foundCbServerUrl	{
		fqdn, err := fqdn.FqdnHostname()
		if err != nil || fqdn == "localhost" {
			log.Errorf("Error getting fqdn for host: %s", err)
			log.Errorf("Please set cb_server_url explicitly to generate valid links.")
			return fmt.Errorf("Please set cb_server_url explicitly")
		} else {
			config.CbServerURL = fmt.Sprintf("https://%s/", fqdn)
			log.Infof("Automatically set CbServerURL to %s", config.CbServerURL)
		}
	}
	return nil
}

func (config * Configuration) parseDebugSettings(input * ini.File) error {
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
	return nil
}

func (config * Configuration) parseRemoveFromOutput(input * ini.File) error {
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
	return nil
}

func (config * Configuration) parseDebugStore(input * ini.File) error {
	if input.Section("bridge").HasKey("debug_store") {
		key := input.Section("bridge").Key("debug_store")
		config.DebugStore = key.Value()
	} else {
		config.DebugStore = "/var/log/cb/integrations/cb-event-forwarder"
	}

	log.Debugf("Debug Store is %s", config.DebugStore)
	return nil
}

func (config * Configuration) parseHttpServerPort(input * ini.File) error {
	if input.Section("bridge").HasKey("http_server_port") {
		key := input.Section("bridge").Key("http_server_port")
		port, err := strconv.Atoi(key.Value())
		if err == nil {
			config.HTTPServerPort = port
		}
	}
	return nil
}

func (config * Configuration) parseExitTimeout(input * ini.File) error {
	config.ExitTimeoutSeconds = DEFAULTEXITTIMEOUT

	if input.Section("bridge").HasKey("exit_timeout") {
		key := input.Section("bridge").Key("exit_timeout")
		durationSeconds, err := key.Int64()
		if err != nil {
			log.Fatalf("Error parsing exit timeout seconds...%v", err)
		}
		config.ExitTimeoutSeconds = time.Duration(durationSeconds) * time.Second
	}
	return nil
}

func (config * Configuration) parseRabbitmqSettings(input * ini.File)  error {
	if input.Section("bridge").HasKey("rabbit_mq_username") {
		key := input.Section("bridge").Key("rabbit_mq_username")
		config.AMQPUsername = key.Value()
	}

	if !input.Section("bridge").HasKey("rabbit_mq_password") {
		return fmt.Errorf("Missing required rabbit_mq_password section")
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
		AMQPUsername, AMQPPassword, err := GetLocalRabbitMQCredentials()
		if err != nil {
			return err
		}
		config.AMQPUsername = AMQPUsername
		config.AMQPPassword = AMQPPassword
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
			return fmt.Errorf("Unknown value for 'rabbit_mq_automatic_acking': valid values are true, false, 1, 0. Default is 'true'")
		}
	}
	return nil
}

func (config * Configuration) parseCannedInput(input * ini.File) error {

	config.CannedInput = false

	cannedInputEnvVar := os.Getenv("EF_CANNED_INPUT")

	if cannedInputEnvVar != "" {
		config.CannedInput = true
		config.CannedInputLocation = &cannedInputEnvVar
	} else if input.Section("bridge").HasKey("canned_input") {
		key := input.Section("bridge").Key("canned_input")
		boolval, err := key.Bool()
		if err == nil {
			config.CannedInput = boolval
		} else {
			return fmt.Errorf("Unknown value for 'canned_input': valid values are true, false, 1, 0. Default is 'false'")
		}
	}
	return nil
}

func (config * Configuration) parseDryRun(input * ini.File) error {
	config.DryRun = false

	if input.Section("bridge").HasKey("dry_run") {
		key := input.Section("bridge").Key("dry_run")
		boolval, err := key.Bool()
		if err == nil {
			config.DryRun = boolval
		} else {
			return fmt.Errorf("Unknown value for 'dry_run': valid values are true, false, 1, 0. Default is 'false'")
		}
	}
	return nil
}

func (config * Configuration) parseRunConsumer(input * ini.File) error {
	config.RunConsumer = true

	runConsumerEnvVar := os.Getenv("EF_RUN_CONSUMER")

	if runConsumerEnvVar != "" {
		config.RunConsumer, _ = strconv.ParseBool(runConsumerEnvVar)
	} else if input.Section("bridge").HasKey("run_consumer") {
		key := input.Section("bridge").Key("run_consumer")
		boolval, err := key.Bool()
		if err == nil {
			config.RunConsumer = boolval
		} else {
			return fmt.Errorf("Unknown value for 'run_consumer': valid values are true, false, 1, 0. Default is 'false'")
		}
	}
	return nil
}

func (config * Configuration) parseCbServerHostname(input * ini.File) {
	if input.Section("bridge").HasKey("cb_server_hostname") {
		key := input.Section("bridge").Key("cb_server_hostname")
		config.AMQPHostname = key.Value()
	}
}

func (config * Configuration) parseOutputFormat(input * ini.File) {
	if input.Section("bridge").HasKey("output_format") {
		key := input.Section("bridge").Key("output_format")
		val := key.Value()
		val = strings.TrimSpace(val)
		val = strings.ToLower(val)
		if val == "leef" {
			config.OutputFormat = LEEFOutputFormat
		}
	}
}

func (config * Configuration) parseCompression(input * ini.File) error {
	config.FileHandlerCompressData = false

	if input.Section("bridge").HasKey("compress_data") {
		key := input.Section("bridge").Key("compress_data")
		b, err := key.Bool()
		if err == nil {
			config.FileHandlerCompressData = b
		} else {
			return fmt.Errorf("Invalid setting for compress_data")
		}
	}

	if config.FileHandlerCompressData {
		if input.Section("bridge").HasKey("compression_type") {
			key := input.Section("bridge").Key("compression_type")
			keyString := key.String()
			CompressionType, err := CompressionTypeFromString(keyString)
			if err != nil {
				return err
			}
			config.CompressionType = CompressionType
		} else {
			config.CompressionType = GZIPCOMPRESSION
		}
	} else {
		config.CompressionType = NOCOMPRESSION
	}

	config.CompressionLevel = 1

	if input.Section("bridge").HasKey("compression_level") {
		key := input.Section("bridge").Key("compression_level")
		level, err := key.Int()
		if err == nil {
			config.CompressionLevel = level
		} else {
			return fmt.Errorf("Invalid compression level specified")
		}
	}
	return nil
}

func (config * Configuration) parseAuditLog(input * ini.File) {
	config.AuditLog = false

	if input.Section("bridge").HasKey("audit_log") {
		key := input.Section("bridge").Key("audit_log")
		b, err := key.Bool()
		if err == nil {
			config.AuditLog = b
		}
	}
}

func (config * Configuration) parseS3Settings(input * ini.File) error {
		if input.Section("s3").HasKey("credential_profile") {
			key := input.Section("s3").Key("credential_profile")
			profileName := key.Value()
			config.S3CredentialProfileName = &profileName
		}

		config.S3Concurrency = 2

		if input.Section("s3").HasKey("concurrency") {
			key := input.Section("s3").Key("concurrency")
			concurrency, err := key.Int()
			if err == nil && concurrency > 0 && concurrency < 100 {
				config.S3Concurrency = concurrency
			} else {
				return fmt.Errorf("Invalid setting for s3 concurrency -- should be an integer number")
			}
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
			key := input.Section("s3").Key("object_prefix")
			objectPrefix := key.Value()
			config.S3ObjectPrefix = &objectPrefix
		}

		// Optional S3 Endpoint configuration for s3 output to s3-compatible storage like Minio
		if input.Section("s3").HasKey("s3_endpoint") {
			key := input.Section("s3").Key("s3_endpoint")
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
		return nil
}

func (config * Configuration) parseHttpSettings(input * ini.File, errs * ConfigurationError) {
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
	config.ParseOAuthConfiguration(input, errs)

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
}

func (config * Configuration) parseKafkaSettings(input * ini.File) {
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
		config.KafkaMaxRequestSize = 1000000 // sane default from issue 959 on sarama github
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
}

func (config * Configuration) parseSplunkSettings(input * ini.File) {
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

	if input.Section("splunk").HasKey("content_type") {
		key := input.Section("splunk").Key("content_type")
		contentType := key.Value()
		config.HTTPContentType = &contentType
	} else {
		jsonString := "application/json"
		config.HTTPContentType = &jsonString
	}
}

func (config * Configuration) parseUseRawExchange(input * ini.File) error {
	if input.Section("bridge").HasKey("use_raw_sensor_exchange") {
		key := input.Section("bridge").Key("use_raw_sensor_exchange")
		boolval, err := key.Bool()
		if err == nil {
			config.UseRawSensorExchange = boolval
			if boolval {
				log.Warn("Configured to listen on the VMware Carbon Black EDR raw sensor event feed.")
				log.Warn("- This will result in a *large* number of messages output via the event forwarder!")
				log.Warn("- Ensure that raw sensor events are enabled in your EDR server (primary & minion) via")
				log.Warn("  the 'EnableRawSensorDataBroadcast' variable in /etc/cb/cb.conf")
			}
		} else {
			return fmt.Errorf("Unknown value for 'use_raw_sensor_exchange': valid values are true, false, 1, 0")
		}
	}
	return nil
}

func (config * Configuration) parseOutputSettings(input * ini.File, errs * ConfigurationError) {
	var parameterKey string

	if !input.Section("bridge").HasKey("output_type") {
		errs.addErrorString("No output type specified")
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
	case "olds3":
		config.OutputType = OLDS3OutputType
		parameterKey = "s3out"
		config.parseS3Settings(input)
	case "news3", "s3":
		parameterKey = "s3out"
		config.OutputType = S3OutputType
		config.parseS3Settings(input)
	case "http":
		parameterKey = "httpout"
		config.OutputType = HTTPOutputType
		config.parseHttpSettings(input, errs)
	case "syslog":
		parameterKey = "syslogout"
		config.OutputType = SyslogOutputType
	case "kafka":
		config.OutputType = KafkaOutputType
		config.parseKafkaSettings(input)
	case "splunk":
		parameterKey = "splunkout"
		config.OutputType = SplunkOutputType
		config.parseSplunkSettings(input)

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

	config.parseTLSConfigForOutput(outType, input, errs)
	config.parseBundleSettings(outType, input, errs)
}

func (config * Configuration) parseTLSConfigForOutput(outType string, input * ini.File, errs * ConfigurationError) {
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
}

func (config * Configuration) parseBundleSettings(outType string, input * ini.File, errs * ConfigurationError) {
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
}

func (config * Configuration) parseMetrics(input * ini.File) {
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
}

func (config * Configuration) parseMessageProcessorCount(input * ini.File) {
	if input.Section("bridge").HasKey("message_processor_count") {
		key := input.Section("bridge").Key("message_processor_count")
		if numprocessors, err := key.Int(); err == nil {
			config.NumProcessors = int(numprocessors)
		} else {
			config.NumProcessors = runtime.NumCPU()
		}
	}
}

func (config * Configuration) parseTimeSetting(input * ini.File) {
	if input.Section("bridge").HasKey("use_time_float") {
		key := input.Section("bridge").Key("use_time_float")
		usetimefloat, _ := key.Bool()
		config.UseTimeFloat = usetimefloat
	} else {
		config.UseTimeFloat = false
	}
}

func (config * Configuration) setDefaults() {
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
}

func ParseConfig(fn string) (Configuration, error) {
	config := Configuration{}
	errs := ConfigurationError{Empty: true}

	input, err := ini.Load(fn)
	if err != nil {
		return config, err
	}

	config.setDefaults()

	// required values

	config.parseServerName(input)

	config.parseDebugSettings(input)

	config.parseRemoveFromOutput(input)

	config.parseDebugStore(input)

	config.parseHttpServerPort(input)

	config.parseExitTimeout(input)

	err = config.parseRabbitmqSettings(input)
	if err != nil {
		errs.addError(err)
	}

	err = config.parseDryRun(input)
	if err != nil {
		errs.addError(err)
	}

	err = config.parseCannedInput(input)
	if err != nil {
		errs.addError(err)
	}

	err = config.parseRunConsumer(input)
	if err != nil {
		errs.addError(err)
	}

	config.parseCbServerHostname(input)

	err = config.parseCbServerURL(input)
	if err != nil {
		errs.addError(err)
	}

	config.parseOutputFormat(input)

	err = config.parseCompression(input)
	if err != nil {
		errs.addError(err)
	}

	config.parseAuditLog(input)

	config.parseOutputSettings(input, &errs)

	err = config.parseUseRawExchange(input)
	if err != nil {
		errs.addError(err)
	}

	config.parseMessageProcessorCount(input)

	config.parseMetrics(input)

	config.parseTimeSetting(input)

	config.configLogging(input)

	config.parseEventTypes(input)

	outputParameterError := config.validateOutputParameters()
	if outputParameterError != nil {
		errs.addError(outputParameterError)
	}

	if !errs.Empty {
		return config, errs
	}
	return config, nil

}

func (config *Configuration) configLogging(input *ini.File) {
	config.configLoggingLevel(input)
	config.configLoggingDir(input)
	config.configLoggingBackups(input)
	config.configLoggingMaxDays(input)
	config.configLoggingMaxSizeMB(input)
}

func (config *Configuration) configLoggingLevel(input *ini.File) {
	logLevel := log.InfoLevel
	if input.Section("bridge").HasKey("log_level") {
		logLevelKey := input.Section("bridge").Key("log_level")
		logLevelString := logLevelKey.String()
		logLevelParsed, err := log.ParseLevel(logLevelString)
		if err == nil {
			logLevel = logLevelParsed
		} else {
			log.Warn("Log level didn't parse correctly will default to INFO...")
		}
	}
	config.LogLevel = logLevel
}

func (config *Configuration) configLoggingDir(input *ini.File) {
	logDir := DEFAULTLOGLOCATION
	if input.Section("bridge").HasKey("log_dir") {
		logDirKey := input.Section("bridge").Key("log_dir")
		logDir = logDirKey.String()
	}
	config.LogDir = logDir
}

func (config *Configuration) configLoggingMaxSizeMB(input *ini.File) {
	logSizeMB := DEFAULTLOGSIZE
	if input.Section("bridge").HasKey("log_size_mb") {
		logSizeMBKey := input.Section("bridge").Key("log_size_mb")
		logSizeMBParsed, err  := logSizeMBKey.Int()
		if err == nil {
			logSizeMB = logSizeMBParsed
		}
	}
	config.LogSizeMB = Min(Max(logSizeMB, 1), MAXLOGSIZE)
}

func (config *Configuration) configLoggingMaxDays(input *ini.File) {
	logDays := DEFAULTLOGRETAINDAYS
	if input.Section("bridge").HasKey("log_age") {
		logDaysKey := input.Section("bridge").Key("log_age")
		logDaysParsed, err := logDaysKey.Int()
		if err == nil {
			logDays = logDaysParsed
		}

	}
	config.LogMaxAge = Min(Max(logDays, 1), 365)
}

func (config *Configuration) configLoggingBackups(input *ini.File) {
	logBackups := DEFAULTLOGBACKUPS
	if input.Section("bridge").HasKey("log_backups") {
		logBackupsKey := input.Section("bridge").Key("log_backups")
		logBackupsParsed, err := logBackupsKey.Int()
		if err == nil {
			logBackups = logBackupsParsed
		}
	}
	config.LogBackups = Min(Max(logBackups, 1), MAXLOGBACKUPS)
}

func configureTLS(config *Configuration) *tls.Config {
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

func (config *Configuration) validateOutputParameters() error {
	outputParameter := config.OutputParameters
	switch config.OutputType {
	case HTTPOutputType:
		matched, _ := regexp.Match("(http(s)?:\\/\\/)([^:].+)(:\\d+)?", []byte(outputParameter))
		if !matched {
			return fmt.Errorf("httpout destination must be set to a valid http(s):// prefixed url")
		}
	default:
		log.Debugf("No ouptut parameters to validate")
		return nil
	}
	return nil
}

// parseOAuthConfiguration parses OAuth related configuration from input and populates config with
// relevant fields.
func (config *Configuration) ParseOAuthConfiguration(input *ini.File, errs *ConfigurationError) {
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
			oAuthJwtPrivateKey = strings.ReplaceAll(oAuthJwtPrivateKey, "\\n", "\n")
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

func (config *Configuration) MoveFileToDebug(name string) {
	if config.DebugFlag {
		baseName := filepath.Base(name)
		dest := filepath.Join(config.DebugStore, baseName)
		log.Debugf("MoveFileToDebug mv %s %s", name, dest)
		err := os.Rename(name, dest)
		if err != nil {
			log.Debugf("MoveFileToDebug mv error: %v", err)
		}
	}
}

func (config *Configuration) GetAMQPTLSConfigFromConf() *tls.Config {
	if config.AMQPTLSEnabled == true {
		log.Info("Connecting to message bus via TLS...")

		tlscfg := new(tls.Config)

		caCert, err := ioutil.ReadFile(config.AMQPTLSCACert)
		if err != nil {
			log.Fatal(err)
		}
		caCertPool := x509.NewCertPool()
		caCertPool.AppendCertsFromPEM(caCert)
		tlscfg.RootCAs = caCertPool

		cert, err := tls.LoadX509KeyPair(config.AMQPTLSClientCert, config.AMQPTLSClientKey)
		if err != nil {
			log.Fatal(err)
		}
		tlscfg.Certificates = []tls.Certificate{cert}
		tlscfg.InsecureSkipVerify = true
		return tlscfg
	} else {
		return nil
	}
}
