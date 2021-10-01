package logging

import (
	log "github.com/sirupsen/logrus"
	"github.com/natefinch/lumberjack"
	"path"
)

const DEFAULTLOGFILENAME = "cb-event-forwarder.log"

type LogFileHandler struct {
	location         string
	level            log.Level
	fileNameWithPath string
	lumberJackLogger * lumberjack.Logger
}

func NewLogFileHandler(location string, level log.Level, maxSize, maxAge, maxBackups int) LogFileHandler {
	fileNameWithPath := path.Join(location, DEFAULTLOGFILENAME)
	lumberJackLogger := &lumberjack.Logger{
		Filename:   fileNameWithPath,
		MaxSize:    maxSize,
		MaxBackups: maxBackups,
		MaxAge:     maxAge,
		Compress:   true,
		LocalTime: true,
	}
	return LogFileHandler{lumberJackLogger: lumberJackLogger, location: location, level: level, fileNameWithPath: fileNameWithPath}
}

func (lh *LogFileHandler) InitializeLogging() {
	lh.setUpLogger()
}

func (lh *LogFileHandler) setUpLogger() {
	log.SetOutput(lh.lumberJackLogger)
	log.SetLevel(lh.level)
}

func (lh * LogFileHandler) Rotate() {
	lh.lumberJackLogger.Rotate()
}