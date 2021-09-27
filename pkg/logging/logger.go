package logging

import (
	"fmt"
	log "github.com/sirupsen/logrus"
	"os"
	"path"
	"time"
)

const DEFAULTLOGFILENAME = "cb-event-forwarder.log"

type LogFileHandler struct {
	location         string
	level            log.Level
	fileNameWithPath string
}

func NewLogFileHandler(location string, level log.Level) LogFileHandler {
	return LogFileHandler{location: location, level: level, fileNameWithPath: path.Join(location, DEFAULTLOGFILENAME)}
}

func (lh *LogFileHandler) InitializeLogging() {
	lh.rollOldLogFileIfExists()
	lh.setUpLogger()
}

func getTimestampedLogFileName() string {
	return fmt.Sprintf("%s-%s.log", "cb-event-forwarder", time.Now().UTC().Format(time.RFC3339))
}

func (lh *LogFileHandler) getRolledLogFileNameWithPath() string {
	return path.Join(lh.location, getTimestampedLogFileName())
}

func (lh *LogFileHandler) rollOverLogFile() {
	err := os.Rename(lh.fileNameWithPath, lh.getRolledLogFileNameWithPath())
	if err != nil {
		log.Panicf("Couldn't roll log file: %v", err)
	}
}

func (lh *LogFileHandler) rollOldLogFileIfExists() {
	if _, err := os.Stat(lh.fileNameWithPath); err == nil {
		lh.rollOverLogFile()
	}
}

func (lh *LogFileHandler) openForwarderLogFile() (*os.File, error) {
	file, err := os.OpenFile(lh.fileNameWithPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	return file, err
}

func (lh *LogFileHandler) setUpLogger() {
	outputFile, err := lh.openForwarderLogFile()
	if err != nil {
		log.Panicf("Unable to initialize logger %e", err)
	}
	log.Infof("Setting output to %s at %s level", lh.fileNameWithPath, lh.level.String())
	log.SetOutput(outputFile)
	log.SetLevel(lh.level)
}
