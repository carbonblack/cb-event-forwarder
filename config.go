package main

import (
	"errors"
	_ "expvar"
	"fmt"
	"github.com/vaughan0/go-ini"
	"log"
	"strconv"
	"strings"
)

const (
	FileOutputType = iota
	S3OutputType
	TCPOutputType
	UDPOutputType
	SyslogOutputType
)

const (
	LEEFOutputFormat = iota
	JSONOutputFormat
)

type Configuration struct {
	ServerName           string
	AMQPHostname         string
	DebugFlag            bool
	OutputType           int
	OutputFormat         int
	AMQPUsername         string
	AMQPPassword         string
	AMQPPort             int
	OutputParameters     string
	EventTypes           []string
	HTTPServerPort       int
	CbServerURL          string
	UseRawSensorExchange bool

	// this is a hack for S3 specific configuration
	S3ServerSideEncryption  *string
	S3CredentialProfileName *string
	S3ACLPolicy             *string
	S3ObjectPrefix          *string

	// Syslog-specific configuration
	SyslogTLSClientKey  *string
	SyslogTLSClientCert *string
	SyslogTLSCACert     *string
	SyslogTLSVerify     bool
}

type ConfigurationError struct {
	Errors []string
	Empty  bool
}

func (c *Configuration) AMQPURL() string {
	return fmt.Sprintf("amqp://%s:%s@%s:%d", c.AMQPUsername, c.AMQPPassword, c.AMQPHostname, c.AMQPPort)
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
			"watchlist.hit.#",
			"watchlist.storage.hit.#",
		}},
		{"events_feed", []string{
			"feed.ingress.hit.#",
			"feed.storage.hit.#",
			"feed.query.hit.#",
		}},
		{"events_alert", []string{
			"alert.watchlist.hit.#",
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
			"binarystore.file.added",
		}},
	}

	for _, eventType := range eventTypes {
		if eventType.configKey == "events_raw_sensor" && c.UseRawSensorExchange {
			// skip the sensor event section if we're consuming from the firehose anyway
			// we will be forwarding everything in that case
			continue
		}
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
		}
	}

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
		if err != nil {
			config.AMQPPort = port
		}
	}

	if len(config.AMQPUsername) == 0 || len(config.AMQPPassword) == 0 {
		config.AMQPUsername, config.AMQPPassword, err = parseCbConf()
		if err != nil {
			errs.addError(err)
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
		case "syslog":
			parameterKey = "syslogout"
			config.OutputType = SyslogOutputType

			clientKeyFilename, ok := input.Get("syslog", "client_key")
			if ok {
				config.SyslogTLSClientKey = &clientKeyFilename
			}

			clientCertFilename, ok := input.Get("syslog", "client_cert")
			if ok {
				config.SyslogTLSClientCert = &clientCertFilename
			}

			caCertFilename, ok := input.Get("syslog", "ca_cert")
			if ok {
				config.SyslogTLSCACert = &caCertFilename
			}

			config.SyslogTLSVerify = true
			tlsVerify, ok := input.Get("syslog", "tls_verify")
			if ok {
				if tlsVerify == "false" {
					config.SyslogTLSVerify = false
				}
			}

		default:
			errs.addErrorString(fmt.Sprintf("Unknown output type: %s", outType))
		}
	}
	if len(parameterKey) > 0 {
		val, ok = input.Get("bridge", parameterKey)
		if !ok {
			errs.addErrorString(fmt.Sprintf("Missing value for key %s, required by output type %s", val, outType))
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
				log.Println("Configured to listen on the Carbon Black Enterprise Response raw sensor event feed.")
				log.Println("- This will result in a *large* number of messages output via the event forwarder!")
				log.Println("- Ensure that raw sensor events are enabled in your Cb server (master & minion) via")
				log.Println("  the 'EnableRawSensorDataBroadcast' variable in /etc/cb/cb.conf")
			}
		} else {
			errs.addErrorString("Unknown value for 'use_raw_sensor_exchange': valid values are true, false, 1, 0")
		}
	}

	config.parseEventTypes(input)

	if !errs.Empty {
		return config, errs
	} else {
		return config, nil
	}
}
