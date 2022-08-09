package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"github.com/gorilla/mux"
	log "github.com/sirupsen/logrus"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
	"strconv"
	"sync"
	"time"
)

type ServiceCommand struct {
	Command string `json:"command"`
}

type StringDict map[string]interface{}
type StatusResponse struct {
	Succeeded bool       `json:"succeeded"`
	Message   string     `json:"message,omitempty"`
	Data      StringDict `json:"data,omitempty"`
}

var version = ""
var process *exec.Cmd
var servicePid int = -1
var stateFile = "/etc/cb/integrations/event-forwarder/state.dat"
var serviceMutex sync.Mutex
var (
	logLevel = flag.String("loglevel", "info", "log level to use (default is Info)")
)

func GetStatusResponse(writer http.ResponseWriter, request *http.Request) {
	log.Debug("GetStatusResponse")
	writer.Header().Set("Content-Type", "application/json")
	status := GetProcessStatus(false)
	log.WithField("status", status).Debug("Returning")
	json.NewEncoder(writer).Encode(StatusResponse{
		Succeeded: true,
		Data:      StringDict{"state": GetProcessStatus(true)}})
}

func ReadStateFile() string {
	stateBytes, err := ioutil.ReadFile(stateFile)
	if err != nil {
		log.Debug("Could not read state.dat ")
		return "started" // default to started -- we don't want to have customers start EF container and not getting
		// data if this read fails. They won't know it isn't collecting data if we did "stopped"
	}
	return string(bytes.TrimSpace(stateBytes))
}

// hack because os.FindProcess always succeeds on unix systems
func checkPid(pid int) bool {
	out, err := exec.Command("kill", "-s", "0", strconv.Itoa(pid)).CombinedOutput()
	if err != nil {
		log.Println(err)
	}

	if string(out) == "" {
		return true // pid exist
	}
	return false
}

func GetProcessStatus(lock bool) string {

	if lock {
		serviceMutex.Lock()
		defer serviceMutex.Unlock()
	}
	if servicePid > -1 {
		bRunning := checkPid(servicePid)

		if !bRunning {
			log.WithField("servicePid", servicePid).Debug("FindProcess returned error")
			servicePid = -1
			return "stopped"
		}
		log.Debug("Status: Running")
		return "running"
	}
	log.Debug("Status: Stopped (No Pid)")
	return "stopped"
}

func ChangeService(writer http.ResponseWriter, request *http.Request) {
	log.Debug("ChangeService")
	writer.Header().Set("Content-Type", "application/json")

	if GetProcessStatus(false) == "stopped" {
		log.Debug("Starting Service")
		StartService()
		log.Debug("Done issuing Starting Service")
	} else {
		log.Debug("Stopping Service")
		StopService()
		log.Debug("Done Stopping Service")
	}
	newStatus := GetProcessStatus(false)
	log.WithField("newStatus", newStatus).Debug("New status")
	json.NewEncoder(writer).Encode(StatusResponse{
		Succeeded: true,
		Data:      StringDict{"state": string(newStatus)},
	})

}

func StartService() error {
	serviceMutex.Lock()
	defer serviceMutex.Unlock()
	command := "/usr/share/cb/integrations/event-forwarder/cb-event-forwarder"
	if GetProcessStatus(false) == "running" {
		log.Warn("Service was already Running")
		return nil
	}
	pidfile := "/run/cb/integrations/cb-event-forwarder/cb-event-forwarder.pid"
	process = exec.Command(command,
		"-pid-file="+pidfile,
		"/etc/cb/integrations/event-forwarder/cb-event-forwarder.conf")
	err := process.Start()
	time.Sleep(3 * time.Second)
	if err != nil {
		log.WithField("err", err).Error("Event forwarder failed to launch")
		log.Fatal(err)
		return err
	}
	log.Debug("Event forwarder launched")
	pidBytes, err := ioutil.ReadFile(pidfile)
	if err != nil {
		log.Warn("Unable to read from pidfile")
		return nil
	}

	servicePid, err = strconv.Atoi(string(bytes.TrimSpace(pidBytes)))
	if err != nil {
		log.Warn("Unable to convert process pid from pidfile")
		return nil
	}

	log.WithField("service", servicePid).Debug("Service Pid retrieved")
	log.Debug("Writing state.dat - state: started")
	err = ioutil.WriteFile(stateFile, []byte("started"), 0644)
	if err != nil {
		log.Warn("Failed to write state.dat to retain status information")
		return nil
	}

	return nil
}

func StopService() error {
	serviceMutex.Lock()
	defer serviceMutex.Unlock()
	if GetProcessStatus(false) == "stopped" {
		log.WithField("servicePid", servicePid).Warn("Service was already stopped")
		return nil
	}

	localProcess, err := os.FindProcess(servicePid)
	if err != nil {
		log.WithField("servicePid", servicePid).Warn("Unable to find service via pid")
		servicePid = -1
		return nil
	}

	localProcess.Kill()
	process.Process.Release() // global var
	servicePid = -1
	log.Debug("Writing state.dat - state: stopped")
	ioutil.WriteFile(stateFile, []byte("stopped"), 0644)
	if err != nil {
		log.Warn("Failed to write state.dat to retain status information")
	}
	return nil
}

func GetConfig(writer http.ResponseWriter, request *http.Request) {
	log.Debug("In GetConfig")
	writer.Header().Set("Content-Type", "application/json")

	configLocation := "/etc/cb/integrations/event-forwarder/cb-event-forwarder.conf"
	log.WithField("configLocation", configLocation).Debug("Config file")
	configContents, err := ioutil.ReadFile(configLocation)
	if err != nil {
		log.Fatal(err)
	}
	log.Debug("Sending config file")
	err = json.NewEncoder(writer).Encode(StatusResponse{
		Succeeded: true,
		Data:      StringDict{"content": string(configContents)},
	})
	if err != nil {
		return
	}
	return
}

func WriteConfig(writer http.ResponseWriter, request *http.Request) {
	log.Debug("In WriteConfig")
	fi := "/etc/cb/integrations/event-forwarder/cb-event-forwarder.conf"
	body, _ := ioutil.ReadAll(request.Body)
	bodyString := string(body)
	result := EdrFixups(bodyString)
	err := ioutil.WriteFile(fi, []byte(result), 0644)
	if err != nil {
		log.WithField("Error", err).Warn("Could not write config file")
		json.NewEncoder(writer).Encode(StatusResponse{
			Succeeded: false,
		})
		return
	}
	log.Debug("Leaving WriteConfig")

	json.NewEncoder(writer).Encode(StatusResponse{
		Succeeded: true,
	})
	return
}

func CheckConfig(writer http.ResponseWriter, request *http.Request) {
	body, _ := ioutil.ReadAll(request.Body)
	bodyString := string(body)
	filename := "/etc/cb/integrations/event-forwarder/check.tmp"
	result := EdrFixups(bodyString)
	err := ioutil.WriteFile(filename, []byte(result), 0644)
	if err != nil {
		log.WithField("Error", err).Warn("Could not write config file")
		json.NewEncoder(writer).Encode(StatusResponse{
			Succeeded: false,
		})
		return
	}
	command := "/usr/share/cb/integrations/event-forwarder/cb-event-forwarder"
	process := exec.Command(command,
		"-check", filename)
	if err := process.Run(); err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			log.WithField("Exit Code", exitError.ExitCode()).Warn("ConfigCheck says config is invalid")
		} else {
			log.WithField("error", err).Error("cmd.Run for ConfigCheck failed")
		}
		json.NewEncoder(writer).Encode(StatusResponse{
			Succeeded: false,
		})
		return
	}
	log.Debug("No errors found with ConfigCheck")
	json.NewEncoder(writer).Encode(StatusResponse{
		Succeeded: true,
	})
	return
}

// EdrFixups - a bug exists in EDR 7.7.0 where fields not visible in the UI get erased. This is a hack to
// workaround this bug. In containerized instances EDR hostname will ALWAYS be `carbonblack-edr`
func EdrFixups(bodyString string) string {
	m := regexp.MustCompile("(?m)(.*)^cb_server_hostname(.*)$(.*)")
	result := m.ReplaceAllString(bodyString, "${1}cb_server_hostname = carbonblack-edr${3}")
	return result
}

func HandleUpload(writer http.ResponseWriter, request *http.Request) {
	fi := ""
	body, _ := ioutil.ReadAll(request.Body)
	switch request.RequestURI {
	case "/ca_cert":
		log.Info("CA Cert Uploaded")
		fi = "ca-certs.pem"
	case "/aws_credentials":
		log.Info("Aws Creds Uploaded")
		fi = "aws.creds"
	case "/client_cert":
		type ClientCertPayload struct {
			Cert string `json:"cert"`
			Key  string `json:"key"`
		}
		var PayloadJson ClientCertPayload
		err := json.Unmarshal([]byte(body), &PayloadJson)
		if err != nil {
			log.WithField("err", err).Debug(err)
			return
		}
		ioutil.WriteFile("/etc/cb/integrations/event-forwarder/client-cert.pem", []byte(PayloadJson.Cert), 0644)
		ioutil.WriteFile("/etc/cb/integrations/event-forwarder/client-key.pem", []byte(PayloadJson.Key), 0644)
		log.Info("Client Cert Uploaded")
		json.NewEncoder(writer).Encode(StatusResponse{
			Succeeded: true,
		})
		return
	}
	ioutil.WriteFile("/etc/cb/integrations/event-forwarder/"+fi, []byte(body), 0644)
	log.Trace(string(body))
	json.NewEncoder(writer).Encode(StatusResponse{
		Succeeded: true,
	})
	return
}

func InitRouts() *mux.Router {
	router := mux.NewRouter()
	router.HandleFunc("/service", GetStatusResponse).Methods("GET")
	router.HandleFunc("/service", ChangeService).Methods("POST")
	router.HandleFunc("/config", GetConfig).Methods("GET")
	router.HandleFunc("/config", WriteConfig).Methods("POST")
	router.HandleFunc("/check", CheckConfig).Methods("POST")
	router.HandleFunc("/check", CheckConfig).Methods("GET")
	router.HandleFunc("/ca_cert", HandleUpload).Methods("POST")
	router.HandleFunc("/aws_credentials", HandleUpload).Methods("POST")
	router.HandleFunc("/client_cert", HandleUpload).Methods("POST")
	return router
}

func StartRestService() *http.Server {
	service := &http.Server{
		Handler:      InitRouts(),
		Addr:         "0.0.0.0:5744",
		WriteTimeout: 15 * time.Second,
		ReadTimeout:  15 * time.Second,
	}

	go func() {
		if err := service.ListenAndServe(); err != nil {
			log.Println(err)
		}
	}()

	return service
}

func ensureServiceIsRunning() {
	log.Debug("Checking service status.")
	var state string
	state = ReadStateFile()

	if "started" == state {
		processState, _ := process.Process.Wait()
		state = ReadStateFile()
		if "started" == state {
			log.WithField("exitcode", processState.ExitCode()).Info("Event forwarder exited")
			process.Process.Release()

			log.Warn("Process State returns as 'Exited' . Restarting ...")
			StartService()
		}
	}
}

func WaitForShutdownSignal() {
	shutdownChannel := make(chan os.Signal, 1)
	signal.Notify(shutdownChannel, os.Interrupt)

	crashCheckTicker := time.NewTicker(10 * time.Second)
	defer crashCheckTicker.Stop()

	shutdown := false
	for !shutdown {
		select {
		case <-shutdownChannel:
			shutdown = true

		case <-crashCheckTicker.C:
			ensureServiceIsRunning()
		}
	}
}

func ShutdownService(service *http.Server) {
	StopService()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()
	service.Shutdown(ctx)
}

func main() {
	flag.Parse()
	val, _ := log.ParseLevel(*logLevel)
	log.SetLevel(val)
	log.WithField("loglevel", log.GetLevel()).Debug("Log Level set to %d")
	log.Info("Starting event-forwarder v" + version)
	restService := StartRestService()
	if "started" == ReadStateFile() {
		log.Info("Starting up.")
		StartService()
	}
	WaitForShutdownSignal()
	ShutdownService(restService)
	os.Exit(0)
}
