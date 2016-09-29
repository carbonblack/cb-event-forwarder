package main

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"github.com/carbonblack/cb-event-forwarder/sensor_events"
	"github.com/golang/protobuf/proto"
	"github.com/streadway/amqp"
	"io/ioutil"
	"log"
	"strings"
)

func GetProcessGUID(m *sensor_events.CbEventMsg) string {
	if m.Header.ProcessPid != nil && m.Header.ProcessCreateTime != nil && m.Env != nil &&
		m.Env.Endpoint != nil && m.Env.Endpoint.SensorId != nil {
		pid := m.Header.GetProcessPid()
		create_time := m.Header.GetProcessCreateTime()
		sensor_id := m.Env.Endpoint.GetSensorId()

		return MakeGUID(sensor_id, pid, create_time)
	} else {
		return fmt.Sprintf("%d", m.Header.GetProcessGuid())
	}
}

type ConvertedCbMessage struct {
	OriginalMessage *sensor_events.CbEventMsg
}

func (inmsg *ConvertedCbMessage) getStringByGuid(guid int64) (string, error) {
	for _, rawString := range inmsg.OriginalMessage.GetStrings() {
		if rawString.GetGuid() == guid {
			return GetUnicodeFromUTF8(rawString.GetUtf8String()), nil
		}
	}
	return "", errors.New(fmt.Sprintf("Could not find string for id %d", guid))
}

func ProcessProtobufBundle(routingKey string, body []byte, headers amqp.Table) ([]map[string]interface{}, error) {
	msgs := make([]map[string]interface{}, 0, 1)
	var err error

	err = nil
	totalLength := len(body)
	i := 0

	for bytesRead := 0; bytesRead < totalLength; {
		length := (int)(binary.LittleEndian.Uint32(body[bytesRead : bytesRead+4]))

		msg, err := ProcessProtobufMessage(routingKey, body[bytesRead+4:bytesRead+length+4], headers)
		if err != nil {
			log.Printf("Error in ProcessProtobufMessage for event index %d: %s", i, err.Error())
		} else if msg != nil {
			msgs = append(msgs, msg)
		}

		bytesRead += length + 4
		i += 1
	}

	return msgs, err
}

func ProcessRawZipBundle(routingKey string, body []byte, headers amqp.Table) ([]map[string]interface{}, error) {
	msgs := make([]map[string]interface{}, 0, 1)

	bodyReader := bytes.NewReader(body)
	zipReader, err := zip.NewReader(bodyReader, (int64)(len(body)))

	// there are code revisions where the message body is not actually a zip file but rather a series of
	// <length><protobuf> messages. Fixed as of 5.2.0 P4.

	// assume that if zip.NewReader can't recognize the message body as a proper zip file, the body is
	// a protobuf bundle instead.

	if err != nil {
		return ProcessProtobufBundle(routingKey, body, headers)
	}

	for i, zf := range zipReader.File {
		src, err := zf.Open()
		if err != nil {
			log.Printf("Error opening raw sensor event zip file content: %s. Continuing.", err.Error())
			continue
		}
		defer src.Close()

		unzippedFile, err := ioutil.ReadAll(src)
		if err != nil {
			log.Printf("Error opening raw sensor event file id %d from package: %s", i, err.Error())
			continue
		}

		newMsgs, err := ProcessProtobufBundle(routingKey, unzippedFile, headers)
		if err != nil {
			log.Printf("Errors above from processing zip filename %s", zf.Name)
		}
		msgs = append(msgs, newMsgs...)
	}
	return msgs, nil
}

func createEnvMessage(headers amqp.Table) (*sensor_events.CbEnvironmentMsg, error) {
	endpointMsg := &sensor_events.CbEndpointEnvironmentMsg{}
	if hostId, ok := headers["hostId"]; ok {
		if val, ok := hostId.(int64); ok {
			endpointMsg.HostId = &val
		} else {
			return nil, errors.New("Could not parse hostId from message header")
		}
	}
	if hostName, ok := headers["sensorHostName"]; ok {
		if val, ok := hostName.(string); !ok {
			return nil, errors.New("Could not parse sensor host name from message header")
		} else {
			endpointMsg.SensorHostName = &val
		}
	}
	if sensorId, ok := headers["sensorId"]; ok {
		if val, ok := sensorId.(int64); !ok {
			return nil, errors.New("Could not parse sensorId from message header")
		} else {
			sensorId := int32(val)
			endpointMsg.SensorId = &sensorId
		}
	}

	serverMsg := &sensor_events.CbServerEnvironmentMsg{}
	if nodeId, ok := headers["nodeId"]; ok {
		if val, ok := nodeId.(int32); !ok {
			return nil, errors.New("Could not parse nodeId from message header")
		} else {
			serverMsg.NodeId = &val
		}
	}

	return &sensor_events.CbEnvironmentMsg{
		Endpoint: endpointMsg,
		Server:   serverMsg,
	}, nil
}

func ProcessProtobufMessage(routingKey string, body []byte, headers amqp.Table) (map[string]interface{}, error) {
	cbMessage := new(sensor_events.CbEventMsg)
	err := proto.Unmarshal(body, cbMessage)
	if err != nil {
		return nil, err
	}

	if cbMessage.Env == nil {
		// if the Env is nil, try to fill it in using the headers from the AMQP message
		// (the raw sensor exchange does not fill in the SensorEnv or ServerEnv messages)
		cbMessage.Env, err = createEnvMessage(headers)
		if err != nil {
			return nil, err
		}
	}

	inmsg := &ConvertedCbMessage{
		OriginalMessage: cbMessage,
	}

	outmsg := make(map[string]interface{})
	outmsg["timestamp"] = WindowsTimeToUnixTime(inmsg.OriginalMessage.Header.GetTimestamp())
	outmsg["type"] = routingKey

	outmsg["sensor_id"] = cbMessage.Env.Endpoint.GetSensorId()
	outmsg["computer_name"] = cbMessage.Env.Endpoint.GetSensorHostName()

	// is the message from an endpoint event process?
	eventMsg := true

	switch {
	case cbMessage.Process != nil:
		WriteProcessMessage(inmsg, outmsg)
	case cbMessage.Modload != nil:
		WriteModloadMessage(inmsg, outmsg)
	case cbMessage.Filemod != nil:
		WriteFilemodMessage(inmsg, outmsg)
	case cbMessage.Network != nil:
		WriteNetconnMessage(inmsg, outmsg)
	case cbMessage.Regmod != nil:
		WriteRegmodMessage(inmsg, outmsg)
	case cbMessage.Childproc != nil:
		WriteChildprocMessage(inmsg, outmsg)
	case cbMessage.Crossproc != nil:
		WriteCrossProcMessge(inmsg, outmsg)
	case cbMessage.Emet != nil:
		WriteEmetEvent(inmsg, outmsg)
	case cbMessage.NetconnBlocked != nil:
		WriteNetconnBlockedMessage(inmsg, outmsg)
	case cbMessage.TamperAlert != nil:
		eventMsg = false
		WriteTamperAlertMsg(inmsg, outmsg)
	case cbMessage.Blocked != nil:
		eventMsg = false
		WriteProcessBlockedMsg(inmsg, outmsg)
	case cbMessage.Module != nil:
		eventMsg = false
		WriteModinfoMessage(inmsg, outmsg)
	default:
		// we ignore event types we don't understand yet.
		return nil, nil
	}

	// write metadata about the process in case this message is generated by a process on an endpoint
	if eventMsg {
		processGuid := GetProcessGUID(cbMessage)
		outmsg["process_guid"] = processGuid
		outmsg["pid"] = inmsg.OriginalMessage.Header.GetProcessPid()
		if _, ok := outmsg["md5"]; !ok {
			outmsg["md5"] = GetMd5Hexdigest(inmsg.OriginalMessage.Header.GetProcessMd5())
		}

		// add link to process in the Cb UI if the Cb hostname is set
		// TODO: not happy about reaching in to the "config" object for this
		if config.CbServerURL != "" {
			outmsg["link_process"] = fmt.Sprintf("%s#analyze/%s/1", config.CbServerURL, processGuid)
			outmsg["link_sensor"] = fmt.Sprintf("%s#/host/%d", config.CbServerURL, cbMessage.Env.Endpoint.GetSensorId())
		}
	}

	return outmsg, nil
}

func WriteProcessMessage(message *ConvertedCbMessage, kv map[string]interface{}) {
	kv["event_type"] = "proc"

	file_path, _ := message.getStringByGuid(message.OriginalMessage.Header.GetFilepathStringGuid())
	kv["path"] = file_path

	// hack to rewrite the "type" since the Cb server may make incoming process events "ingress.event.process" or
	// "ingress.event.procstart"

	if message.OriginalMessage.Process.GetCreated() {
		kv["type"] = "ingress.event.procstart"
		if message.OriginalMessage.Process.Md5Hash != nil {
			kv["md5"] = GetMd5Hexdigest(message.OriginalMessage.Process.GetMd5Hash())
		}
	} else {
		kv["type"] = "ingress.event.procend"
	}

	kv["command_line"] = GetUnicodeFromUTF8(message.OriginalMessage.Process.GetCommandline())

	om := message.OriginalMessage

	if om.Env != nil && om.Env.Endpoint != nil && om.Env.Endpoint.SensorId != nil && om.Process.ParentPid != nil &&
		om.Process.ParentCreateTime != nil {
		kv["parent_process_guid"] = MakeGUID(om.Env.Endpoint.GetSensorId(), om.Process.GetParentPid(),
			om.Process.GetParentCreateTime())
	} else {
		kv["parent_process_guid"] = fmt.Sprintf("%d", om.Process.GetParentGuid())
	}

	// add link to process in the Cb UI if the Cb hostname is set
	if config.CbServerURL != "" {
		kv["link_parent"] = fmt.Sprintf("%s#analyze/%s/1", config.CbServerURL, kv["parent_process_guid"])
	}

	if message.OriginalMessage.Process.Username != nil {
		kv["username"] = message.OriginalMessage.Process.GetUsername()
	}
}

func WriteModloadMessage(message *ConvertedCbMessage, kv map[string]interface{}) {
	kv["event_type"] = "modload"
	kv["type"] = "ingress.event.moduleload"

	file_path, _ := message.getStringByGuid(message.OriginalMessage.Header.GetFilepathStringGuid())
	kv["path"] = file_path
	kv["md5"] = GetMd5Hexdigest(message.OriginalMessage.Modload.GetMd5Hash())

}

func filemodAction(a sensor_events.CbFileModMsg_CbFileModAction) string {
	switch a {
	case sensor_events.CbFileModMsg_actionFileModCreate:
		return "create"
	case sensor_events.CbFileModMsg_actionFileModWrite:
		return "write"
	case sensor_events.CbFileModMsg_actionFileModDelete:
		return "delete"
	case sensor_events.CbFileModMsg_actionFileModLastWrite:
		return "lastwrite"
	}
	return fmt.Sprintf("unknown (%d)", int32(a))
}

func WriteFilemodMessage(message *ConvertedCbMessage, kv map[string]interface{}) {
	kv["event_type"] = "filemod"
	kv["type"] = "ingress.event.filemod"

	file_path, _ := message.getStringByGuid(message.OriginalMessage.Header.GetFilepathStringGuid())
	kv["path"] = file_path

	action := message.OriginalMessage.Filemod.GetAction()
	kv["action"] = filemodAction(action)
	kv["actiontype"] = int32(action)
}

func WriteChildprocMessage(message *ConvertedCbMessage, kv map[string]interface{}) {
	kv["event_type"] = "childproc"
	kv["type"] = "ingress.event.childproc"

	kv["created"] = message.OriginalMessage.Childproc.GetCreated()

	om := message.OriginalMessage
	if om.Childproc.Pid != nil && om.Childproc.CreateTime != nil && om.Env != nil &&
		om.Env.Endpoint != nil && om.Env.Endpoint.SensorId != nil {
		pid := om.Childproc.GetPid()
		create_time := om.Childproc.GetCreateTime()
		sensor_id := om.Env.Endpoint.GetSensorId()

		// for some reason, the Childproc.pid field is an int64 and not an int32 as it is in the process header
		// convert the pid to int32
		pid32 := int32(pid & 0xffffffff)

		kv["child_process_guid"] = MakeGUID(sensor_id, pid32, create_time)
	} else {
		kv["child_process_guid"] = om.Childproc.GetChildGuid()
	}

	// add link to process in the Cb UI if the Cb hostname is set
	if config.CbServerURL != "" {
		kv["link_child"] = fmt.Sprintf("%s#analyze/%s/1", config.CbServerURL, kv["child_process_guid"])
	}

	kv["md5"] = GetMd5Hexdigest(message.OriginalMessage.Childproc.GetMd5Hash())
}

func regmodAction(a sensor_events.CbRegModMsg_CbRegModAction) string {
	switch a {
	case sensor_events.CbRegModMsg_actionRegModCreateKey:
		return "createkey"
	case sensor_events.CbRegModMsg_actionRegModWriteValue:
		return "writeval"
	case sensor_events.CbRegModMsg_actionRegModDeleteKey:
		return "delkey"
	case sensor_events.CbRegModMsg_actionRegModDeleteValue:
		return "delval"
	}
	return fmt.Sprintf("unknown (%d)", int32(a))
}

func WriteRegmodMessage(message *ConvertedCbMessage, kv map[string]interface{}) {
	kv["event_type"] = "regmod"
	kv["type"] = "ingress.event.regmod"

	kv["path"] = GetUnicodeFromUTF8(message.OriginalMessage.Regmod.GetUtf8Regpath())

	action := message.OriginalMessage.Regmod.GetAction()
	kv["action"] = regmodAction(action)
	kv["actiontype"] = int32(action)
}

func WriteNetconnMessage(message *ConvertedCbMessage, kv map[string]interface{}) {
	kv["event_type"] = "netconn"
	kv["type"] = "ingress.event.netconn"

	kv["domain"] = GetUnicodeFromUTF8(message.OriginalMessage.Network.GetUtf8Netpath())
	kv["ipv4"] = GetIPv4Address(message.OriginalMessage.Network.GetIpv4Address())
	kv["port"] = ntohs(uint16(message.OriginalMessage.Network.GetPort()))
	kv["protocol"] = int32(message.OriginalMessage.Network.GetProtocol())

	if message.OriginalMessage.Network.GetOutbound() {
		kv["direction"] = "outbound"
	} else {
		kv["direction"] = "inbound"
	}

	//
	// In CB 5.1 local and remote ip/port were added.  They aren't guaranteed
	// to be there (b/c we have an older sensor) or in some cases we cannot
	// determine them

	if message.OriginalMessage.Network.RemoteIpAddress != nil {
		kv["remote_ip"] = GetIPv4Address(message.OriginalMessage.Network.GetRemoteIpAddress())
		kv["remote_port"] = ntohs(uint16(message.OriginalMessage.Network.GetRemotePort()))
	}

	if message.OriginalMessage.Network.LocalIpAddress != nil {
		kv["local_ip"] = GetIPv4Address(message.OriginalMessage.Network.GetLocalIpAddress())
		kv["local_port"] = ntohs(uint16(message.OriginalMessage.Network.GetLocalPort()))
	}
}

func WriteModinfoMessage(message *ConvertedCbMessage, kv map[string]interface{}) {
	kv["event_type"] = "binary_info"
	kv["type"] = "ingress.event.module"

	kv["md5"] = strings.ToUpper(string(message.OriginalMessage.Module.GetMd5()))
	kv["size"] = message.OriginalMessage.Module.GetOriginalModuleLength()

	digsigResult := make(map[string]interface{})
	digsigResult["result"] = message.OriginalMessage.Module.GetUtf8_DigSig_Result()

	kv["digsig"] = digsigResult
}

func emetMitigationType(a *sensor_events.CbEmetMitigationAction) string {

	mitigation := a.GetMitigationType()

	switch mitigation {
	case sensor_events.CbEmetMitigationAction_actionDep:
		return "Dep"
	case sensor_events.CbEmetMitigationAction_actionSehop:
		return "Sehop"
	case sensor_events.CbEmetMitigationAction_actionAsr:
		return "Asr"
	case sensor_events.CbEmetMitigationAction_actionAslr:
		return "Aslr"
	case sensor_events.CbEmetMitigationAction_actionNullPage:
		return "NullPage"
	case sensor_events.CbEmetMitigationAction_actionHeapSpray:
		return "HeapSpray"
	case sensor_events.CbEmetMitigationAction_actionMandatoryAslr:
		return "MandatoryAslr"
	case sensor_events.CbEmetMitigationAction_actionEaf:
		return "Eaf"
	case sensor_events.CbEmetMitigationAction_actionEafPlus:
		return "EafPlus"
	case sensor_events.CbEmetMitigationAction_actionBottomUpAslr:
		return "BottomUpAslr"
	case sensor_events.CbEmetMitigationAction_actionLoadLibrary:
		return "LoadLibrary"
	case sensor_events.CbEmetMitigationAction_actionMemoryProtection:
		return "MemoryProtection"
	case sensor_events.CbEmetMitigationAction_actionSimulateExecFlow:
		return "SimulateExecFlow"
	case sensor_events.CbEmetMitigationAction_actionStackPivot:
		return "StackPivot"
	case sensor_events.CbEmetMitigationAction_actionCallerChecks:
		return "CallerChecks"
	case sensor_events.CbEmetMitigationAction_actionBannedFunctions:
		return "BannedFunctions"
	case sensor_events.CbEmetMitigationAction_actionDeepHooks:
		return "DeepHooks"
	case sensor_events.CbEmetMitigationAction_actionAntiDetours:
		return "AntiDetours"
	}
	return fmt.Sprintf("unknown (%d)", int32(mitigation))
}

func WriteEmetEvent(message *ConvertedCbMessage, kv map[string]interface{}) {
	kv["event_type"] = "emet_mitigation"
	kv["type"] = "ingress.event.emetmitigation"

	kv["log_message"] = message.OriginalMessage.Emet.GetActionText()
	kv["mitigation"] = emetMitigationType(message.OriginalMessage.Emet.GetAction())
	kv["blocked"] = message.OriginalMessage.Emet.GetBlocked()
	kv["log_id"] = message.OriginalMessage.Emet.GetEmetId()
	kv["emet_timestamp"] = message.OriginalMessage.Emet.GetEmetTimstamp()
}

func crossprocOpenType(a sensor_events.CbCrossProcessOpenMsg_OpenType) string {
	switch a {
	case sensor_events.CbCrossProcessOpenMsg_OpenProcessHandle:
		return "open_process"
	case sensor_events.CbCrossProcessOpenMsg_OpenThreadHandle:
		return "open_thread"
	}
	return fmt.Sprintf("unknown (%d)", int32(a))
}

func WriteCrossProcMessge(message *ConvertedCbMessage, kv map[string]interface{}) {
	kv["event_type"] = "cross_process"

	om := message.OriginalMessage

	if message.OriginalMessage.Crossproc.Open != nil {
		open := message.OriginalMessage.Crossproc.Open
		kv["type"] = "ingress.event.crossprocopen"

		kv["cross_process_type"] = crossprocOpenType(open.GetType())

		kv["requested_acces"] = open.GetRequestedAccess()
		kv["target_pid"] = open.GetTargetPid()
		kv["target_create_time"] = open.GetTargetProcCreateTime()
		kv["target_md5"] = GetMd5Hexdigest(open.GetTargetProcMd5())
		kv["target_path"] = open.GetTargetProcPath()

		pid32 := int32(open.GetTargetPid() & 0xffffffff)
		kv["target_process_guid"] = MakeGUID(om.Env.Endpoint.GetSensorId(), pid32, int64(open.GetTargetProcCreateTime()))
	} else {
		rt := message.OriginalMessage.Crossproc.Remotethread
		kv["type"] = "ingress.event.remotethread"

		kv["cross_process_type"] = "remote_thread"
		kv["target_pid"] = rt.GetRemoteProcPid()
		kv["target_create_time"] = rt.GetRemoteProcCreateTime()
		kv["target_md5"] = GetMd5Hexdigest(rt.GetRemoteProcMd5())
		kv["target_path"] = rt.GetRemoteProcPath()

		kv["target_process_guid"] = MakeGUID(om.Env.Endpoint.GetSensorId(), int32(rt.GetRemoteProcPid()), int64(rt.GetRemoteProcCreateTime()))
	}

	// add link to process in the Cb UI if the Cb hostname is set
	if config.CbServerURL != "" {
		kv["link_target"] = fmt.Sprintf("%s#analyze/%s/1", config.CbServerURL, kv["target_process_guid"])
	}
}

func tamperAlertType(a sensor_events.CbTamperAlertMsg_CbTamperAlertType) string {
	switch a {
	case sensor_events.CbTamperAlertMsg_AlertCoreDriverUnloaded:
		return "CoreDriverUnloaded"
	case sensor_events.CbTamperAlertMsg_AlertNetworkDriverUnloaded:
		return "NetworkDriverUnloaded"
	case sensor_events.CbTamperAlertMsg_AlertCbServiceStopped:
		return "CbServiceStopped"
	case sensor_events.CbTamperAlertMsg_AlertCbProcessTerminated:
		return "CbProcessTerminated"
	case sensor_events.CbTamperAlertMsg_AlertCbCodeInjection:
		return "CbCodeInjection"
	}
	return fmt.Sprintf("unknown (%d)", int32(a))
}

func WriteTamperAlertMsg(message *ConvertedCbMessage, kv map[string]interface{}) {
	kv["event_type"] = "tamper"
	kv["type"] = "ingress.event.tamper"

	kv["tamper_type"] = tamperAlertType((message.OriginalMessage.TamperAlert.GetType()))
}

func blockedProcessEventType(a sensor_events.CbProcessBlockedMsg_BlockEvent) string {
	switch a {
	case sensor_events.CbProcessBlockedMsg_ProcessCreate:
		return "ProcessCreate"
	case sensor_events.CbProcessBlockedMsg_RunningProcess:
		return "RunningProcess"
	}
	return fmt.Sprintf("unknown (%d)", int32(a))
}

func blockedProcessResult(a sensor_events.CbProcessBlockedMsg_BlockResult) string {
	switch a {
	case sensor_events.CbProcessBlockedMsg_ProcessTerminated:
		return "ProcessTerminated"
	case sensor_events.CbProcessBlockedMsg_NotTerminatedCBProcess:
		return "NotTerminatedCBProcess"
	case sensor_events.CbProcessBlockedMsg_NotTerminatedSystemProcess:
		return "NotTerminatedSystemProcess"
	case sensor_events.CbProcessBlockedMsg_NotTerminatedCriticalSystemProcess:
		return "NotTerminatedCriticalSystemProcess"
	case sensor_events.CbProcessBlockedMsg_NotTerminatedWhitelistedPath:
		return "NotTerminatedWhitelistPath"
	case sensor_events.CbProcessBlockedMsg_NotTerminatedOpenProcessError:
		return "NotTerminatedOpenProcessError"
	case sensor_events.CbProcessBlockedMsg_NotTerminatedTerminateError:
		return "NotTerminatedTerminateError"
	}
	return fmt.Sprintf("unknown (%d)", int32(a))
}

func WriteProcessBlockedMsg(message *ConvertedCbMessage, kv map[string]interface{}) {
	block := message.OriginalMessage.Blocked
	kv["event_type"] = "blocked_process"
	kv["type"] = "ingress.event.processblock"

	if block.GetBlockedType() == sensor_events.CbProcessBlockedMsg_MD5Hash {
		kv["blocked_reason"] = "Md5Hash"
	} else {
		kv["blocked_reason"] = fmt.Sprintf("unknown (%d)", int32(block.GetBlockedType()))
	}

	kv["blocked_event"] = blockedProcessEventType(block.GetBlockedEvent())
	kv["md5"] = GetMd5Hexdigest(block.GetBlockedmd5Hash())
	kv["path"] = block.GetBlockedPath()
	kv["blocked_result"] = blockedProcessResult(block.GetBlockResult())

	if block.GetBlockResult() == sensor_events.CbProcessBlockedMsg_NotTerminatedOpenProcessError ||
		block.GetBlockResult() == sensor_events.CbProcessBlockedMsg_NotTerminatedTerminateError {
		kv["blocked_error"] = block.GetBlockError()
	}

	if block.BlockedPid != nil {
		kv["pid"] = block.GetBlockedPid()
		kv["process_create_time"] = block.GetBlockedProcCreateTime()

		om := message.OriginalMessage
		kv["process_guid"] = MakeGUID(om.Env.Endpoint.GetSensorId(), int32(block.GetBlockedPid()), int64(block.GetBlockedProcCreateTime()))
		// add link to process in the Cb UI if the Cb hostname is set
		if config.CbServerURL != "" {
			kv["link_target"] = fmt.Sprintf("%s#analyze/%s/1", config.CbServerURL, kv["target_process_guid"])
		}
	}

	kv["command_line"] = block.GetBlockedCmdline()

	if block.GetBlockedEvent() == sensor_events.CbProcessBlockedMsg_ProcessCreate &&
		block.GetBlockResult() == sensor_events.CbProcessBlockedMsg_ProcessTerminated {
		kv["uid"] = block.GetBlockedUid()
		kv["username"] = block.GetBlockedUsername()
	}
}

func WriteNetconnBlockedMessage(message *ConvertedCbMessage, kv map[string]interface{}) {
	kv["event_type"] = "blocked_netconn"
	// TODO: need ingress event type for netconn blocks

	blocked := message.OriginalMessage.NetconnBlocked

	kv["domain"] = GetUnicodeFromUTF8(blocked.GetUtf8Netpath())
	kv["ipv4"] = GetIPv4Address(blocked.GetIpv4Address())
	kv["port"] = ntohs(uint16(blocked.GetPort()))
	kv["protocol"] = int32(blocked.GetProtocol())

	if blocked.GetOutbound() {
		kv["direction"] = "outbound"
	} else {
		kv["direction"] = "inbound"
	}
	if blocked.RemoteIpAddress != nil {
		kv["remote_ip"] = GetIPv4Address(blocked.GetRemoteIpAddress())
		kv["remote_port"] = ntohs(uint16(blocked.GetRemotePort()))
	}

	if blocked.LocalIpAddress != nil {
		kv["local_ip"] = GetIPv4Address(blocked.GetLocalIpAddress())
		kv["local_port"] = ntohs(uint16(blocked.GetLocalPort()))
	}
}
