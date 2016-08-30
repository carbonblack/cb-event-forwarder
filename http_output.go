package main

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

type HttpFormatter interface {
	Format(string) string
}

/* This is an implementation of the HttpFormatter interface which is specific to the SensorAlertsApi */
type SensorAlertsFormatter struct {
}

/* this function takes the json string specified by the event parameter and transforms it into a format
   which the SensorAlertsApi can consume */
func (this *SensorAlertsFormatter) Format(event string) string {
	return `{ "service" : "carbonblack", "alerts" : [ ` + event + ` ] }`
}

/* This is the HTTP implementation of the OutputHandler interface defined in main.go */
type HttpOutput struct {
	dest        string
	eventSentAt time.Time
	headers     map[string]string
	ssl         bool

	lastHttpError     string
	lastHttpErrorTime time.Time
	httpErrors        int64

	client    *http.Client
	request   *http.Request
	formatter HttpFormatter
}

type HttpStatistics struct {
	LastSendTime  time.Time `json:"last_send_time"`
	Destination   string    `json:"destination"`
	HttpErrors    int64     `json:"http_errors"`
	LastErrorTime time.Time `json:"last_error_time"`
	LastErrorText string    `json:"last_error_text"`
}

/* Construct the HttpOutput object */
func (this *HttpOutput) Initialize(dest string) error {

	this.headers = make(map[string]string)
	this.dest = dest

	/* add authorization token, if applicable */
	if config.HttpAuthorizationToken != nil {
		this.headers["Authorization"] = *config.HttpAuthorizationToken
	}

	/* send the requests as json */
	this.headers["Content-Type"] = "application/json"

	/* ssl verification is enabled by default */
	if this.ssl {
		this.client = &http.Client{}
	} else { /* disable ssl verification */
		transport := &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}
		this.client = &http.Client{Transport: transport}
	}

	this.eventSentAt = time.Time{}
	return nil
}

/* this function processes messages indefinitely from the channel specified by the messages parameter */
func (this *HttpOutput) Go(messages <-chan string, errorChan chan<- error) error {

	go func() {

		/* allow various signals through to this function */
		interrupt := make(chan os.Signal, 1)
		hangup := make(chan os.Signal, 1)
		quit := make(chan os.Signal, 1)
		terminate := make(chan os.Signal, 1)

		signal.Notify(interrupt, syscall.SIGINT)
		signal.Notify(hangup, syscall.SIGHUP)
		signal.Notify(quit, syscall.SIGQUIT)
		signal.Notify(terminate, syscall.SIGTERM)

		defer signal.Stop(interrupt)
		defer signal.Stop(hangup)
		defer signal.Stop(quit)
		defer signal.Stop(terminate)

		/* process messages until we receive a signal from the OS */
		for {
			select {

			/* read message from channel */
			case message := <-messages:

				/* format the message if need be */
				if this.formatter != nil {
					message = this.formatter.Format(message)
				}

				if err := this.output(message); err != nil {
					errorChan <- err
					return
				}

			/* received SIGINT */
			case <-interrupt:
				log.Println("HttpOutput received SIGINT. Shutting down...")
				return

			/* received SIGHUP */
			case <-hangup:
				log.Println("HttpOutput received SIGHUP. Shutting down...")
				return

			/* received SIGQUIT */
			case <-quit:
				log.Println("HttpOutput received SIGQUIT. Shutting down...")
				return

			/* received SIGTERM */
			case <-terminate:
				log.Println("HttpOutput received SIGTERM. Shutting down...")
				return
			}
		}
	}()
	return nil
}

func (this *HttpOutput) String() string {
	return this.dest
}

func (this *HttpOutput) Statistics() interface{} {
	return HttpStatistics{
		LastSendTime:  this.eventSentAt,
		Destination:   this.dest,
		LastErrorTime: this.lastHttpErrorTime,
		LastErrorText: this.lastHttpError,
		HttpErrors:    this.httpErrors,
	}
}

func (this *HttpOutput) Key() string {
	return this.dest
}

/* This function takes a string representing an event as parameter and returns an error denoting whether or
   not the function was successful. The function does a POST of the given event to this.dest */
func (this *HttpOutput) output(event string) error {

	var err error = nil

	/* Initialize the POST */
	this.request, err = http.NewRequest("POST", this.dest, bytes.NewBuffer([]byte(event)))

	/* Set the header values of the post */
	for key, value := range this.headers {
		this.request.Header.Set(key, value)
	}

	/* Execute the POST */
	resp, err := this.client.Do(this.request)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	/* Some sort of issue with the POST */
	if resp.StatusCode != 200 {
		body, _ := ioutil.ReadAll(resp.Body)
		errorData := resp.Status + "\n" + string(body)

		this.httpErrors += 1
		this.lastHttpError = errorData
		this.lastHttpErrorTime = time.Now()

		log.Printf("%s\n", errorData)

		return fmt.Errorf("Error: HTTP request failed with status: %d", resp.StatusCode)
	}

	fmt.Println("response Status:", resp.Status)
	fmt.Println("response Headers:", resp.Header)
	body, _ := ioutil.ReadAll(resp.Body)
	fmt.Println("response Body:", string(body))

	this.eventSentAt = time.Now()
	return err
}
