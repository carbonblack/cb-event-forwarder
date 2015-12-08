package main

import (
	"errors"
	_ "expvar"
	"fmt"
	"github.com/vaughan0/go-ini"
	"strconv"
	"strings"
)

const (
	FileOutputType = iota
	S3OutputType
	TCPOutputType
	UDPOutputType
)

const (
	LEEFOutputFormat = iota
	JSONOutputFormat
)

type Configuration struct {
	ServerName       string
	AMQPHostname     string
	DebugFlag        bool
	OutputType       int
	OutputFormat     int
	AMQPUsername     string
	AMQPPassword     string
	OutputParameters string
	EventTypes       []string
	HTTPServerPort   int
}

type ConfigurationError struct {
	Errors []string
	Empty  bool
}

func (c *Configuration) AMQPURL() string {
	return fmt.Sprintf("amqp://%s:%s@%s:5004", c.AMQPUsername, c.AMQPPassword, c.AMQPHostname)
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
		}},
		{"events_binary_observed", []string{
			"binaryinfo.#",
		}},
		{"events_binary_upload", []string{
			"binarystore.file.added",
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
	config.HTTPServerPort = 33706

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
		if err != nil {
			config.HTTPServerPort = port
		}
	}

	val, ok = input.Get("bridge", "rabbit_mq_username")
	if !ok {
		errs.addErrorString("Missing required rabbit_mq_username section")
	} else {
		config.AMQPUsername = val
	}

	val, ok = input.Get("bridge", "rabbit_mq_password")
	if !ok {
		errs.addErrorString("Missing required rabbit_mq_password section")
	} else {
		config.AMQPPassword = val
	}

	if len(config.AMQPUsername) == 0 && len(config.AMQPPassword) == 0 {
		config.AMQPUsername, config.AMQPPassword, err = parseCbConf()
		if err != nil {
			errs.addError(err)
		}
	}

	val, ok = input.Get("bridge", "cb_server_hostname")
	if ok {
		config.AMQPHostname = val
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

	config.parseEventTypes(input)

	if !errs.Empty {
		return config, errs
	} else {
		return config, nil
	}
}
