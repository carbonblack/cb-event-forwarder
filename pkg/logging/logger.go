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
}

func NewLogFileHandler(location string, level log.Level) LogFileHandler {
	return LogFileHandler{location: location, level: level, fileNameWithPath: path.Join(location, DEFAULTLOGFILENAME)}
}

func (lh *LogFileHandler) InitializeLogging() {
	lh.setUpLogger()
}

func (lh *LogFileHandler) setUpLogger() {
	log.SetOutput(&lumberjack.Logger{
		Filename:   lh.fileNameWithPath,
		MaxSize:    50,
		MaxBackups: 7,
		MaxAge:     1,
		Compress:   true,
		LocalTime: true,
	})
	log.SetLevel(lh.level)
}
