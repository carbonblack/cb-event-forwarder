package main

import (
	"text/template"
	"github.com/carbonblack/cb-event-forwarder/internal/util"
	"errors"
	"fmt"
	"regexp"
)

func Mutate(value interface {}, cbmsg map [string] interface{}, keys ... string)  error {
	var ref map[string] interface{} = cbmsg
	var tref interface{}
	var err error = nil
	var ok bool = false
	klen := len(keys)
	if klen == 0 {
		return errors.New("Delete needs keys")
	}
	if klen > 1 {
		tref, err = util.MapGetByArray(cbmsg, keys[:klen-1])
		if ref,ok = tref.(map[string] interface{}); ! ok {
			ref = make(map[string] interface{})
		}
	}
	if err == nil {
		last_key := keys[klen - 1]
		 if _ , ok := ref[last_key]; ok{
			 ref[last_key] = value
			return nil
		 }
		 return errors.New(fmt.Sprintf("Mutate couldn't get last key"))
	} else {
		return err
	}
}

func Delete(cbmsg map [string] interface{}, keys ... string)  error {
	var ref map[string] interface{} = cbmsg
	var tref interface{}
	var err error = nil
	klen := len(keys)
	if klen == 0 {
		return errors.New("Delete needs keys")
	}
	if klen > 1 {
		tref, err = util.MapGetByArray(cbmsg, keys[:klen-1])
		ref = tref.(map[string] interface{})
	}
	if err == nil {
		last_key := keys[klen - 1]
		 if _ , ok := ref[last_key]; ok{
			 delete(ref,last_key)
			return nil
		 }
		 return errors.New(fmt.Sprintf("Delete couldn't get last key"))
	} else {
		return err
	}
}

func RegexMatch(s string,r string) bool {
	_, err := regexp.MatchString(s,r)
	return err == nil
}

func GetFuncMap() template.FuncMap {
	funcMap := template.FuncMap{"RegexMatch":RegexMatch,"MapGetByArray" : util.MapGetByArray ,"Delete": Delete,"Mutate" : Mutate,"LeefFormat": util.Leef, "CefFormat" : util.Cef , "JsonFormat" : util.Json , "YamlFormat" : util.Yaml, "GetCurrentTimeFormat"  : util.GetCurrentTimeFormat,  "GetCurrentTimeRFC3339" : util.GetCurrentTimeRFC3339 , "GetCurrentTimeUnix" : util.GetCurrentTimeUnix, "GetCurrentTimeUTC" : util.GetCurrentTimeUTC, "ParseTime" : util.ParseTime}
	return funcMap
}