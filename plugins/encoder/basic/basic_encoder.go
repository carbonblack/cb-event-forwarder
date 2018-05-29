package main

import (
	"text/template"
	"github.com/carbonblack/cb-event-forwarder/internal/util"
)

func GetFuncMap() template.FuncMap {
	funcMap := template.FuncMap{"LeefFormat": util.Leef, "CefFormat" : util.Cef , "JsonFormat" : util.Json , "YamlFormat" : util.Yaml, "GetCurrentTimeFormat"  : util.GetCurrentTimeFormat,  "GetCurrentTimeRFC3339" : util.GetCurrentTimeRFC3339 , "GetCurrentTimeUnix" : util.GetCurrentTimeUnix, "GetCurrentTimeUTC" : util.GetCurrentTimeUTC, "ParseTime" : util.ParseTime}
	return funcMap
}