package protobufmessageprocessor

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"fmt"
	. "github.com/carbonblack/cb-event-forwarder/pkg/config"
	. "github.com/carbonblack/cb-event-forwarder/pkg/sensorevents"
	. "github.com/carbonblack/cb-event-forwarder/pkg/utils"
	"github.com/mailru/easyjson"
	log "github.com/sirupsen/logrus"
	"github.com/streadway/amqp"
	"google.golang.org/protobuf/proto"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func UnixTimestampFromWindowsTime(time int64, useTimeFloat bool) (unixTime UnixTimeStamp) {
	unixTime.Timestamp = convertToUnixTime(time, useTimeFloat)
	return unixTime
}

func ParentCreateTimestampFromWindowsTime(time int64, useTimeFloat bool) (unixTime ParentCreateTime) {
	unixTime.Timestamp = convertToUnixTime(time, useTimeFloat)
	return unixTime
}

func convertToUnixTime(time int64, useTimeFloat bool) interface{} {
	var timestamp interface{}
	if useTimeFloat {
		timestamp = WindowsTimeToUnixTimeFloat(time)
	} else {
		timestamp = WindowsTimeToUnixTime(time)
	}
	return timestamp
}

func md5OrUseHeader(md5 string, msg *CbEventMsg) string {
	if md5 != "" {
		return md5
	}
	return md5FromHeader(msg)
}

func sha256OrUseHeader(sha256 string, msg *CbEventMsg) string {
	if sha256 != "" {
		return sha256
	}
	return sha256FromHeader(msg)
}

func md5FromHeader(msg *CbEventMsg) string {
	md5 := GetMd5Hexdigest(msg.Header.GetProcessMd5())
	return md5
}

func sha256FromHeader(msg *CbEventMsg) string {
	sha256 := GetSha256Hexdigest(msg.Header.GetProcessSha256())
	return sha256
}

func NewHeaderHashes(msg *CbEventMsg) HeaderHashes {
	md5 := md5FromHeader(msg)
	sha256 := sha256FromHeader(msg)
	return HeaderHashes{Md5: md5, Sha256: sha256}
}

func (pbm *ProtobufMessageProcessor) NewEventMessage(msg *CbEventMsg) *EventMessage {
	processGUID := GetProcessGUID(msg)
	process_guid := processGUID
	pid := msg.Header.GetProcessPid()
	fork_pid := int32(0)
	if msg.Header.ForkPid != nil {
		fork_pid = msg.Header.GetForkPid()
	}
	process_path := msg.Header.GetProcessPath()

	link_process := ""
	link_sensor := ""
	if pbm.Config.CbServerURL != "" {

		link_process = FastStringConcat(
			pbm.Config.CbServerURL, "#analyze/", processGUID, "/0")

		link_sensor = FastStringConcat(
			pbm.Config.CbServerURL, "#/host/", strconv.Itoa(int(msg.Env.Endpoint.GetSensorId())))
	}
	return &EventMessage{ForkPid: fork_pid, Pid: pid, ProcessGuid: process_guid, LinkProcess: link_process, LinkSensor: link_sensor, ProcessPath: process_path}
}

func (pbm *ProtobufMessageProcessor) NewEventMessageWithHashes(msg *CbEventMsg) *EventMessageWithHashes {
	eventMsg := pbm.NewEventMessage(msg)
	headerHashes := NewHeaderHashes(msg)
	return &EventMessageWithHashes{EventMessage: eventMsg, HeaderHashes: &headerHashes}
}

func (m *ModloadMessage) getAsJson() ([]byte, error) {
	return easyjson.Marshal(m)
}

func (pbm *ProtobufMessageProcessor) NewBaseMessage(msg *CbEventMsg, routingKey, eventType string) *BaseEvent {
	timestamp := UnixTimestampFromWindowsTime(msg.Header.GetTimestamp(), pbm.Config.UseTimeFloat)
	typeFromRoutingKey := routingKey
	sensorId := msg.Env.Endpoint.GetSensorId()
	computerName := msg.Env.Endpoint.GetSensorHostName()
	serverName := pbm.Config.ServerName
	return &BaseEvent{EventType: eventType, CbServer: serverName, UnixTimeStamp: timestamp, Type: typeFromRoutingKey, SensorId: sensorId, ComputerName: computerName}
}

func (pbm ProtobufMessageProcessor) NewModLoadMessage(msg *CbEventMsg, routingKey string) *ModloadMessage {
	base := pbm.NewBaseMessage(msg, routingKey, "modload")
	base.Type = "ingress.event.moduleload"
	filePath, _ := getStringByGUID(msg, msg.Header.GetFilepathStringGuid())
	md5 := md5OrUseHeader(GetMd5Hexdigest(msg.Modload.GetMd5Hash()), msg)
	sha256 := sha256OrUseHeader(GetSha256Hexdigest(msg.Modload.GetSha256Hash()), msg)
	eventMessage := pbm.NewEventMessage(msg)
	return &ModloadMessage{EventMessage: eventMessage, BaseEvent: base, Path: filePath, Md5: md5, Sha256: sha256}
}

func (m *ProcessEvent) getAsJson() ([]byte, error) {
	return easyjson.Marshal(m)
}

func (pbm *ProtobufMessageProcessor) NewProcessEvent(msg *CbEventMsg, routingKey string) *ProcessEvent {
	base := pbm.NewBaseMessage(msg, routingKey, "proc")

	path, _ := getStringByGUID(msg, msg.Header.GetFilepathStringGuid())

	sha256 := ""
	md5 := ""
	if msg.Process.GetCreated() {
		base.Type = "ingress.event.procstart"
		if msg.Process.Md5Hash != nil {
			md5 = GetMd5Hexdigest(msg.Process.GetMd5Hash())
		} else {
			md5 = md5FromHeader(msg)
		}
		if msg.Process.Sha256Hash != nil {
			sha256 = GetSha256Hexdigest(msg.Process.GetSha256Hash())
		} else {
			sha256 = sha256FromHeader(msg)
		}

	} else {
		base.Type = "ingress.event.procend"
	}

	command_line := GetUnicodeFromUTF8(msg.Process.GetCommandline())

	parent_path := msg.Process.GetParentPath()
	parent_pid := msg.Process.GetParentPid()
	parent_guid := msg.Process.GetParentGuid()
	parent_create_time := ParentCreateTimestampFromWindowsTime(msg.Process.GetParentCreateTime(), pbm.Config.UseTimeFloat)
	filtering_known_dlls := msg.Process.GetBFilteringKnownDlls()

	parent_md5 := ""
	if msg.Process.ParentMd5 != nil {
		parent_md5 = GetMd5Hexdigest(msg.Process.GetParentMd5())
	}

	parent_sha256 := ""
	if msg.Process.ParentSha256 != nil {
		parent_sha256 = GetSha256Hexdigest(msg.Process.GetParentSha256())
	}

	expect_followon_w_md5 := msg.Process.GetExpectFollowonWMd5()

	parent_process_guid := ""

	if msg.Env != nil && msg.Env.Endpoint != nil && msg.Env.Endpoint.SensorId != nil && msg.Process.ParentPid != nil &&
		msg.Process.ParentCreateTime != nil {
		parent_process_guid = MakeGUID(msg.Env.Endpoint.GetSensorId(), msg.Process.GetParentPid(),
			msg.Process.GetParentCreateTime())
	} else {
		parent_process_guid = fmt.Sprintf("%d", msg.Process.GetParentGuid())
	}

	// add link to process in the Cb UI if the Cb hostname is set
	link_parent := ""
	if pbm.Config.CbServerURL != "" {
		link_parent = fmt.Sprintf("%s#analyze/%s/1", pbm.Config.CbServerURL, parent_process_guid)
	}

	username := ""
	if msg.Process.Username != nil {
		username = msg.Process.GetUsername()
	}

	uid := ""
	if msg.Process.Uid != nil {
		uid = msg.Process.GetUid()
	}

	eventMsg := pbm.NewEventMessage(msg)

	return &ProcessEvent{EventMessage: eventMsg, BaseEvent: base, FilteringKnownDLLS: filtering_known_dlls, CommandLine: command_line, Path: path, Md5: md5, Sha256: sha256, Uid: uid, Username: username, LinkParent: link_parent, ExpectFollowonWMd5: expect_followon_w_md5, ParentSha256: parent_sha256, ParentMd5: parent_md5, ParentCreateTime: parent_create_time, ParentGuid: parent_guid, ParentPath: parent_path, ParentPid: parent_pid}
}

func (f *FilemodEvent) getAsJson() ([]byte, error) {
	return easyjson.Marshal(f)
}

func (pbm *ProtobufMessageProcessor) NewFilemodEvent(msg *CbEventMsg, routingKey string) *FilemodEvent {
	base := pbm.NewBaseMessage(msg, routingKey, "filemod")
	base.Type = "ingress.event.filemod"
	path, _ := getStringByGUID(msg, msg.Header.GetFilepathStringGuid())

	action := msg.Filemod.GetAction()
	fileModAction := filemodAction(action)
	fileModActionType := int32(action)

	fileType := msg.Filemod.GetType()
	filetype := int32(fileType)
	filetype_name := strings.TrimPrefix(CbFileModMsg_CbFileType_name[int32(fileType)], "filetype")

	file_md5 := ""
	if msg.Filemod.Md5Hash != nil {
		file_md5 = GetMd5Hexdigest(msg.Filemod.GetMd5Hash())
	}
	file_sha256 := ""
	if msg.Filemod.Sha256Hash != nil {
		file_sha256 = GetSha256Hexdigest(msg.Filemod.GetSha256Hash())
	}
	tamper := msg.Filemod.GetTamper()
	tamper_sent := msg.Filemod.GetTamperSent()
	eventMessageWithHashes := pbm.NewEventMessageWithHashes(msg)
	return &FilemodEvent{EventMessageWithHashes: eventMessageWithHashes, BaseEvent: base, Path: path, FileType: filetype, Action: fileModAction, ActionType: fileModActionType, FileTypeName: filetype_name, FileMd5: file_md5, FileSha256: file_sha256, Tamper: tamper, TamperSent: tamper_sent}
}

func (n *NetworkV2Event) getAsJson() ([]byte, error) {
	return easyjson.Marshal(n)
}

func (pbm *ProtobufMessageProcessor) NewNetworkV2Event(msg *CbEventMsg, routingKey string) *NetworkV2Event {
	base := pbm.NewBaseMessage(msg, routingKey, "netconn")
	base.Type = "ingress.event.netconn"
	domain := GetUnicodeFromUTF8(msg.Networkv2.GetUtf8Netpath())
	protocol := int32(msg.Networkv2.GetProtocol())

	direction := ""
	if msg.Networkv2.GetOutbound() {
		direction = "outbound"
	} else {
		direction = "inbound"
	}

	proxy := false
	proxy_ip := ""
	proxy_port := uint16(0)
	proxy_domain := ""

	if msg.Networkv2.GetProxyConnection() {
		proxy = true
		proxy_ip = GetIPAddress(msg.Networkv2.GetProxyIpAddress())
		proxy_port = Ntohs(uint16(msg.Networkv2.GetProxyPort()))
		proxy_domain = msg.Networkv2.GetProxyNetPath()
	}

	remote_ip := GetIPAddress(msg.Networkv2.GetRemoteIpAddress())
	remote_port := Ntohs(uint16(msg.Networkv2.GetRemotePort()))

	local_ip := GetIPAddress(msg.Networkv2.GetLocalIpAddress())
	local_port := Ntohs(uint16(msg.Networkv2.GetLocalPort()))

	ja3 := ""
	if msg.Networkv2.Ja3 != nil {
		ja3 = msg.Networkv2.GetJa3()
	}
	ja3s := ""
	if msg.Networkv2.Ja3S != nil {
		ja3s = msg.Networkv2.GetJa3S()
	}
	eventMessageWithHashes := pbm.NewEventMessageWithHashes(msg)
	return &NetworkV2Event{EventMessageWithHashes: eventMessageWithHashes, BaseEvent: base, Ja3: ja3, Ja3s: ja3s, LocalIP: local_ip, LocalPort: local_port, RemoteIP: remote_ip, RemotePort: remote_port, Proxy: proxy, ProxyIP: proxy_ip, ProxyPort: proxy_port, ProxyDomain: proxy_domain, Direction: direction, Domain: domain, Protocol: protocol}
}

func (n *NetconEvent) getAsJson() ([]byte, error) {
	return easyjson.Marshal(n)
}

func (pbm *ProtobufMessageProcessor) NewNetconEvent(msg *CbEventMsg, routingKey string) *NetconEvent {
	base := pbm.NewBaseMessage(msg, routingKey, "netconn")
	base.Type = "ingress.event.netconn"
	domain := GetUnicodeFromUTF8(msg.Network.GetUtf8Netpath())
	ipv4 := GetIPv4Address(msg.Network.GetIpv4Address())
	port := Ntohs(uint16(msg.Network.GetPort()))
	protocol := int32(msg.Network.GetProtocol())

	direction := "inbound"
	if msg.Network.GetOutbound() {
		direction = "outbound"
	}

	remote_ip := ""
	remote_port := uint16(0)
	if msg.Network.RemoteIpAddress != nil {
		remote_ip = GetIPv4Address(msg.Network.GetRemoteIpAddress())
		remote_port = Ntohs(uint16(msg.Network.GetRemotePort()))
	}

	local_ip := ""
	local_port := uint16(0)
	if msg.Network.LocalIpAddress != nil {
		local_ip = GetIPv4Address(msg.Network.GetLocalIpAddress())
		local_port = Ntohs(uint16(msg.Network.GetLocalPort()))
	}
	eventMessageWithHashes := pbm.NewEventMessageWithHashes(msg)
	return &NetconEvent{EventMessageWithHashes: eventMessageWithHashes, BaseEvent: base, LocalIp: local_ip, LocalPort: local_port, RemoteIp: remote_ip, RemotePort: remote_port, Direction: direction, Domain: domain, Ipv4: ipv4, Port: port, Protocol: protocol}
}

func (r *RegmodEvent) getAsJson() ([]byte, error) {
	return easyjson.Marshal(r)
}

func (pbm *ProtobufMessageProcessor) NewRegmodEvent(msg *CbEventMsg, routingKey string) *RegmodEvent {
	base := pbm.NewBaseMessage(msg, routingKey, "regmod")
	base.Type = "ingress.event.regmod"
	path := GetUnicodeFromUTF8(msg.Regmod.GetUtf8Regpath())

	action := msg.Regmod.GetAction()
	regmod_action := regmodAction(action)
	actiontype := int32(action)
	tamper := msg.Regmod.GetTamper()
	tamper_sent := msg.Regmod.GetTamperSent()
	eventMessageWithHashes := pbm.NewEventMessageWithHashes(msg)
	return &RegmodEvent{EventMessageWithHashes: eventMessageWithHashes, BaseEvent: base, Path: path, Action: regmod_action, ActionType: actiontype, Tamper: tamper, TamperSent: tamper_sent}
}

func (c *ChildprocEvent) getAsJson() ([]byte, error) {
	return easyjson.Marshal(c)
}

func (pbm *ProtobufMessageProcessor) NewChildprocEvent(msg *CbEventMsg, routingKey string) *ChildprocEvent {
	base := pbm.NewBaseMessage(msg, routingKey, "childproc")
	base.Type = "ingress.event.childproc"
	created := msg.Childproc.GetCreated()

	child_process_guid := ""
	if msg.Childproc.Pid != nil && msg.Childproc.CreateTime != nil && msg.Env != nil &&
		msg.Env.Endpoint != nil && msg.Env.Endpoint.SensorId != nil {
		pid := msg.Childproc.GetPid()
		createTime := msg.Childproc.GetCreateTime()
		sensorID := msg.Env.Endpoint.GetSensorId()

		// for some reason, the Childproc.pid field is an int64 and not an int32 as it is in the process header
		// convert the pid to int32
		pid32 := int32(pid & 0xffffffff)

		child_process_guid = MakeGUID(sensorID, pid32, createTime)
	} else {
		child_process_guid = fmt.Sprintf("%d", msg.Childproc.GetChildGuid())
	}

	child_pid := msg.Childproc.GetPid()
	tamper := msg.Childproc.GetTamper()
	tamper_sent := msg.Childproc.GetTamperSent()

	sensorID := msg.Env.Endpoint.GetSensorId()
	parent_guid := ""
	if msg.Header.ProcessCreateTime != nil && msg.Header.ProcessPid != nil {
		processCreateTime := msg.Header.GetProcessCreateTime()
		processPid := msg.Header.GetProcessPid()
		parent_guid = MakeGUID(sensorID, processPid, processCreateTime)
	} else {
		parent_guid = fmt.Sprintf("%d", msg.Childproc.GetParentGuid())
	}

	// add link to process in the Cb UI if the Cb hostname is set
	link_child := ""
	if pbm.Config.CbServerURL != "" {
		link_child = fmt.Sprintf("%s#analyze/%s/1", pbm.Config.CbServerURL, child_process_guid)
	}

	path := msg.Childproc.GetPath()
	md5 := md5OrUseHeader(GetMd5Hexdigest(msg.Childproc.GetMd5Hash()), msg)
	sha256 := sha256OrUseHeader(GetSha256Hexdigest(msg.Childproc.GetSha256Hash()), msg)

	childProcType := msg.Childproc.GetChildProcType()
	childproc_type := strings.TrimPrefix(CbChildProcessMsg_CbChildProcType_name[int32(childProcType)],
		"childProc")

	// handle suppressed children
	child_suppressed := false
	child_command_line := ""
	child_username := ""
	if msg.Childproc.Suppressed != nil &&
		msg.Childproc.Suppressed.GetBIsSuppressed() {
		child_suppressed = true
		child_command_line = GetUnicodeFromUTF8(msg.Childproc.GetCommandline())
		child_username = msg.Childproc.GetUsername()
	}
	eventMessage := pbm.NewEventMessage(msg)
	return &ChildprocEvent{EventMessage: eventMessage, BaseEvent: base, ParentGuid: parent_guid, LinkChild: link_child, Created: created, Tamper: tamper, TamperSent: tamper_sent, Path: path, Md5: md5, Sha256: sha256, ChildprocType: childproc_type, ChildSuppressed: child_suppressed, ChildCommandLine: child_command_line, ChildUsername: child_username, ChildPid: child_pid}
}

func (c *CrossprocEvent) getAsJson() ([]byte, error) {
	return easyjson.Marshal(c)
}

func (pbm *ProtobufMessageProcessor) NewCrossprocEvent(msg *CbEventMsg, routingKey string) *CrossprocEvent {
	base := pbm.NewBaseMessage(msg, routingKey, "cross_process")
	base.Type = "ingress.event.crossproc"
	is_target := msg.Crossproc.GetIsTarget()

	cross_process_type := ""
	requested_access := uint32(0)
	target_pid := uint32(0)
	target_create_time := uint64(0)
	target_md5 := ""
	target_sha256 := ""
	target_path := ""
	target_process_guid := ""

	if msg.Crossproc.Open != nil {
		open := msg.Crossproc.Open
		base.Type = "ingress.event.crossprocopen"
		cross_process_type = crossprocOpenType(open.GetType())
		requested_access = open.GetRequestedAccess()
		target_pid = open.GetTargetPid()
		target_create_time = open.GetTargetProcCreateTime()
		target_md5 = GetMd5Hexdigest(open.GetTargetProcMd5())
		target_sha256 = GetSha256Hexdigest(open.GetTargetProcSha256())
		target_path = open.GetTargetProcPath()

		pid32 := int32(open.GetTargetPid() & 0xffffffff)
		target_process_guid = MakeGUID(msg.Env.Endpoint.GetSensorId(), pid32, int64(open.GetTargetProcCreateTime()))
	} else {
		rt := msg.Crossproc.Remotethread
		base.Type = "ingress.event.remotethread"
		cross_process_type = "remote_thread"
		target_pid = rt.GetRemoteProcPid()
		target_create_time = rt.GetRemoteProcCreateTime()
		target_md5 = GetMd5Hexdigest(rt.GetRemoteProcMd5())
		target_sha256 = GetSha256Hexdigest(rt.GetRemoteProcSha256())
		target_path = rt.GetRemoteProcPath()
		target_process_guid = MakeGUID(msg.Env.Endpoint.GetSensorId(), int32(rt.GetRemoteProcPid()), int64(rt.GetRemoteProcCreateTime()))
	}

	link_target := ""
	if pbm.Config.CbServerURL != "" {
		link_target = fmt.Sprintf("%s#analyze/%s/1", pbm.Config.CbServerURL, target_process_guid)
	}
	eventMessageWithHashes := pbm.NewEventMessageWithHashes(msg)
	return &CrossprocEvent{EventMessageWithHashes: eventMessageWithHashes, BaseEvent: base, IsTarget: is_target, CrossProcessType: cross_process_type, RequestedAccess: requested_access, LinkTarget: link_target, TargetMd5: target_md5, TargetSha256: target_sha256, TargetPath: target_path, TargetPid: target_pid, TargetCreateTime: target_create_time, TargetProcessGuid: target_process_guid}
}

func (e *EmetEvent) getAsJson() ([]byte, error) {
	return easyjson.Marshal(e)
}

func (pbm *ProtobufMessageProcessor) NewEmetMessage(msg *CbEventMsg, routingKey string) *EmetEvent {
	base := pbm.NewBaseMessage(msg, routingKey, "emet_mitigation")
	base.Type = "ingress.event.emetmitigation"
	log_message := msg.Emet.GetActionText()
	mitigation := emetMitigationType(msg.Emet.GetAction())
	blocked := msg.Emet.GetBlocked()
	log_id := msg.Emet.GetEmetId()
	emet_timestamp := msg.Emet.GetEmetTimstamp()
	eventMessageWithHashes := pbm.NewEventMessageWithHashes(msg)
	return &EmetEvent{EventMessageWithHashes: eventMessageWithHashes, BaseEvent: base, LogMessage: log_message, Mitigation: mitigation, Blocked: blocked, LogId: log_id, EmetTimestamp: emet_timestamp}
}

func (s *ScriptExEvent) getAsJson() ([]byte, error) {
	return easyjson.Marshal(s)
}

func (pbm *ProtobufMessageProcessor) NewScriptExEvent(msg *CbEventMsg, routingKey string) *ScriptExEvent {
	base := pbm.NewBaseMessage(msg, routingKey, "filelessscriptload")
	base.Type = "ingress.event.filelessscriptload"
	script := msg.Scriptexe.GetScript()
	script_sha256 := msg.Scriptexe.GetSha256()
	eventMessageWithHashes := pbm.NewEventMessageWithHashes(msg)
	return &ScriptExEvent{EventMessageWithHashes: eventMessageWithHashes, BaseEvent: base, Script: script, ScriptSha256: script_sha256}
}

func (t *TamperAlert) getAsJson() ([]byte, error) {
	return easyjson.Marshal(t)
}

func (pbm *ProtobufMessageProcessor) NewTamperAlert(msg *CbEventMsg, routingKey string) *TamperAlert {
	base := pbm.NewBaseMessage(msg, routingKey, "tamper")
	base.Type = "ingress.event.tamper"
	tamper_type := tamperAlertType(msg.TamperAlert.GetType())
	return &TamperAlert{BaseEvent: base, TamperType: tamper_type}
}

func getStringByGUID(msg *CbEventMsg, guid int64) (string, error) {
	for _, rawString := range msg.GetStrings() {
		if rawString.GetGuid() == guid {
			return GetUnicodeFromUTF8(rawString.GetUtf8String()), nil
		}
	}
	return "", fmt.Errorf("could not find string for id %d", guid)
}

func (b *BlockedEvent) getAsJson() ([]byte, error) {
	return easyjson.Marshal(b)
}

func (pbm ProtobufMessageProcessor) NewBlockedEvent(msg *CbEventMsg, routingKey string) *BlockedEvent {
	base := pbm.NewBaseMessage(msg, routingKey, "blocked_process")
	base.Type = "ingress.event.processblocked"
	block := msg.Blocked
	blocked_reason := ""
	if block.GetBlockedType() == CbProcessBlockedMsg_MD5Hash {
		blocked_reason = "Md5Hash"
	} else {
		blocked_reason = fmt.Sprintf("unknown (%d)", int32(block.GetBlockedType()))
	}
	blocked_event := blockedProcessEventType(block.GetBlockedEvent())
	md5 := GetMd5Hexdigest(block.GetBlockedmd5Hash())
	path := block.GetBlockedPath()
	blocked_result := blockedProcessResult(block.GetBlockResult())

	blocked_error := uint32(0)
	if block.GetBlockResult() == CbProcessBlockedMsg_NotTerminatedOpenProcessError ||
		block.GetBlockResult() == CbProcessBlockedMsg_NotTerminatedTerminateError {
		blocked_error = block.GetBlockError()
	}

	pid := int32(0)
	process_create_time := uint64(0)
	process_guid := ""
	link_target := ""
	if block.BlockedPid != nil {
		pid = block.GetBlockedPid()
		process_create_time = block.GetBlockedProcCreateTime()

		process_guid = MakeGUID(msg.Env.Endpoint.GetSensorId(), block.GetBlockedPid(), int64(block.GetBlockedProcCreateTime()))
		// add link to process in the Cb UI if the Cb hostname is set
		if pbm.Config.CbServerURL != "" {
			link_target = fmt.Sprintf("%s#analyze/%s/1", pbm.Config.CbServerURL, process_guid)
		}
	}

	command_line := block.GetBlockedCmdline()
	uid := ""
	username := ""
	if block.GetBlockedEvent() == CbProcessBlockedMsg_ProcessCreate &&
		block.GetBlockResult() == CbProcessBlockedMsg_ProcessTerminated {
		uid = block.GetBlockedUid()
		username = block.GetBlockedUsername()
	}
	return &BlockedEvent{BaseEvent: base, Md5: md5, Path: path, LinkTarget: link_target, ProcessGuid: process_guid, ProcessCreateTime: process_create_time, Pid: pid, CommandLine: command_line, Uid: uid, Username: username, BlockedError: blocked_error, BlockedEvent: blocked_event, BlockedReason: blocked_reason, BlockedResult: blocked_result}
}

func (m *ModInfoEvent) getAsJson() ([]byte, error) {
	return easyjson.Marshal(m)
}

func (pbm ProtobufMessageProcessor) NewModinfoEvent(msg *CbEventMsg, routingKey string) *ModInfoEvent {
	base := pbm.NewBaseMessage(msg, routingKey, "binary_info")
	base.Type = "ingress.event.module"

	md5 := strings.ToUpper(string(msg.Module.GetMd5()))
	sha256 := strings.ToUpper(string(msg.Module.GetSha256()))
	size := msg.Module.GetOriginalModuleLength()

	utf8_copied_module_length := msg.Module.GetCopiedModuleLength()
	utf8_file_description := msg.Module.GetUtf8_FileDescription()
	utf8_company_name := msg.Module.GetUtf8_CompanyName()
	utf8_product_name := msg.Module.GetUtf8_ProductName()
	utf8_file_version := msg.Module.GetUtf8_FileVersion()
	utf8_comments := msg.Module.GetUtf8_Comments()
	utf8_legal_copyright := msg.Module.GetUtf8_LegalCopyright()
	utf8_legal_trademark := msg.Module.GetUtf8_LegalTrademark()
	utf8_internal_name := msg.Module.GetUtf8_InternalName()
	utf8_original_file_name := msg.Module.GetUtf8_OriginalFileName()
	utf8_product_description := msg.Module.GetUtf8_ProductDescription()
	utf8_product_version := msg.Module.GetUtf8_ProductVersion()
	utf8_private_build := msg.Module.GetUtf8_PrivateBuild()
	utf8_special_build := msg.Module.GetUtf8_SpecialBuild()
	icon := msg.Module.GetIcon()
	image_file_header := msg.Module.GetImageFileHeader()
	utf8_on_disk_filename := msg.Module.GetUtf8_OnDiskFilename()

	result := msg.Module.GetUtf8_DigSig_Result()
	publisher := msg.Module.GetUtf8_DigSig_Publisher()
	program_name := msg.Module.GetUtf8_DigSig_ProgramName()
	issuer_name := msg.Module.GetUtf8_DigSig_IssuerName()
	subject_name := msg.Module.GetUtf8_DigSig_SubjectName()
	result_code := msg.Module.GetUtf8_DigSig_ResultCode()
	sign_time := msg.Module.GetUtf8_DigSig_SignTime()
	digsig := DigSigResult{Result: result, Publisher: publisher, ProgramName: program_name, IssuerName: issuer_name, SubjectName: subject_name, ResultCode: result_code, SignTime: sign_time}

	return &ModInfoEvent{BaseEvent: base, Utf8CopiedModuleLength: utf8_copied_module_length, Utf8FileDescription: utf8_file_description, Utf8CompanyName: utf8_company_name, Utf8Comments: utf8_comments, Utf8FileVersion: utf8_file_version, Utf8LegalCopyRight: utf8_legal_copyright, Utf8LegalTradeMark: utf8_legal_trademark, Utf8OriginalFileName: utf8_original_file_name, Utf8InternalName: utf8_internal_name, Utf8ProductName: utf8_product_name, Utf8ProductDescription: utf8_product_description, Utf8ProductVersion: utf8_product_version, Utf8SpecialBuild: utf8_special_build, Utf8PrivateBuild: utf8_private_build, Utf8OnDiskFileName: utf8_on_disk_filename, ImageFileHeader: image_file_header, Icon: icon, Md5: md5, Sha256: sha256, Size: size, Digsig: &digsig}
}

type ProtobufMessageProcessor struct {
	Config *Configuration
}

func NewProtobufMessageProcessor(conf *Configuration) ProtobufMessageProcessor {
	return ProtobufMessageProcessor{Config: conf}
}

func (pbm ProtobufMessageProcessor) fromProtobufMessage(msg *CbEventMsg, routingKey string) (event Event, err error) {
	switch {
	case msg.Process != nil:
		return pbm.NewProcessEvent(msg, routingKey), nil
	case msg.Modload != nil:
		return pbm.NewModLoadMessage(msg, routingKey), nil
	case msg.Filemod != nil:
		return pbm.NewFilemodEvent(msg, routingKey), nil
	case msg.Networkv2 != nil:
		return pbm.NewNetworkV2Event(msg, routingKey), nil
	case msg.Network != nil:
		return pbm.NewNetconEvent(msg, routingKey), nil
	case msg.Regmod != nil:
		return pbm.NewRegmodEvent(msg, routingKey), nil
	case msg.Childproc != nil:
		return pbm.NewChildprocEvent(msg, routingKey), nil
	case msg.Crossproc != nil:
		return pbm.NewCrossprocEvent(msg, routingKey), nil
	case msg.Emet != nil:
		return pbm.NewEmetMessage(msg, routingKey), nil
	case msg.Scriptexe != nil:
		return pbm.NewScriptExEvent(msg, routingKey), nil
	case msg.TamperAlert != nil:
		return pbm.NewTamperAlert(msg, routingKey), nil
	case msg.Blocked != nil:
		return pbm.NewBlockedEvent(msg, routingKey), nil
	case msg.Module != nil:
		return pbm.NewModinfoEvent(msg, routingKey), nil
	default:
		return nil, nil
	}
}

func (pbm ProtobufMessageProcessor) isEventEnabled(msg *CbEventMsg) bool {
	switch {
	case msg.Process != nil:
		if _, ok := pbm.Config.EventMap["ingress.event.process"]; ok {
			return true
		} else if _, ok := pbm.Config.EventMap["ingress.event.procstart"]; ok {
			return true
		} else if _, ok := pbm.Config.EventMap["ingress.event.procend"]; ok {
			return true
		} else {
			return false
		}
	case msg.Modload != nil:
		if _, ok := pbm.Config.EventMap["ingress.event.moduleload"]; ok {
			return true
		}
		return false
	case msg.Filemod != nil:
		if _, ok := pbm.Config.EventMap["ingress.event.filemod"]; ok {
			return true
		}
		return false

	case msg.Networkv2 != nil:
		if _, ok := pbm.Config.EventMap["ingress.event.netconn"]; ok {
			return true
		}
		return false
	case msg.Network != nil:
		if _, ok := pbm.Config.EventMap["ingress.event.netconn"]; ok {
			return true
		}
		return false
	case msg.Regmod != nil:
		if _, ok := pbm.Config.EventMap["ingress.event.regmod"]; ok {
			return true
		}
		return false
	case msg.Childproc != nil:
		if _, ok := pbm.Config.EventMap["ingress.event.childproc"]; ok {
			return true
		}
		return false
	case msg.Crossproc != nil:
		if _, ok := pbm.Config.EventMap["ingress.event.crossprocopen"]; ok {
			return true
		}
		return false
	case msg.Emet != nil:
		if _, ok := pbm.Config.EventMap["ingress.event.emetmitigation"]; ok {
			return true
		}
		return false
	case msg.Scriptexe != nil:
		if _, ok := pbm.Config.EventMap["ingress.event.filelessscriptload"]; ok {
			return true
		}
		return false
	case msg.NetconnBlockedv2 != nil:
		// generated by sensor, but not used in CbR (CB-23994)
		log.Debugf("ignoring unhandled event netconnblockedv2")
		return false
	case msg.NetconnBlocked != nil:
		// generated by sensor, but not used in CbR (CB-23994)
		log.Debugf("ignoring unhandled event netconnblocked")
		return false
	case msg.TamperAlert != nil:
		if _, ok := pbm.Config.EventMap["ingress.event.tamper"]; ok {
			return true
		}
		return false
	case msg.Blocked != nil:
		if _, ok := pbm.Config.EventMap["ingress.event.processblock"]; ok {
			return true
		}
		return false
	case msg.Module != nil:
		if _, ok := pbm.Config.EventMap["ingress.event.module"]; ok {
			return true
		}
		return false
	default:
		// we ignore event types we don't understand yet.
		return false
	}
}

func (pbm ProtobufMessageProcessor) ProcessProtobufBundle(routingKey string, body []byte, headers amqp.Table) ([][]byte, error) {
	msgs := make([][]byte, 0, 1)
	var err error

	err = nil
	totalLength := uint64(len(body))
	i := 0

	if totalLength < 4 {
		err = fmt.Errorf("error in ProcessProtobufBundle: body length is < 4 bytes. Giving up")
		return msgs, err
	}

	env, err := CreateEnvMessage(headers)
	if err != nil {
		return nil, err
	}

	var bytesRead uint64
	for bytesRead = 0; bytesRead+4 < totalLength; {
		messageLength := (uint64)(binary.LittleEndian.Uint32(body[bytesRead : bytesRead+4]))
		bytesRead += 4

		if messageLength+bytesRead > totalLength {
			err = fmt.Errorf("error in ProcessProtobufBundle for event index %d: Length %d exceeds %d; giving up",
				i, messageLength, totalLength)
			break
		}

		msg, err := pbm.ProcessProtobufMessageWithEnv(routingKey, body[bytesRead:bytesRead+messageLength], headers, env)
		if err != nil {
			log.Debugf("Error in ProcessProtobufBundle for event index %d: %s. Continuing to next message",
				i, err.Error())
		} else if msg != nil {
			msgs = append(msgs, msg)
		}

		bytesRead += messageLength
		i++
	}

	if err != nil && bytesRead < totalLength {
		err = fmt.Errorf("error in ProcessProtobufBundle: messages did not fill entire bundle; %d bytes left",
			totalLength-bytesRead)
	}

	return msgs, err
}

func (pbm ProtobufMessageProcessor) ProcessRawZipBundle(routingKey string, body []byte, headers amqp.Table) ([][]byte, error) {
	msgs := make([][]byte, 0, 1)

	bodyReader := bytes.NewReader(body)
	zipReader, err := zip.NewReader(bodyReader, (int64)(len(body)))

	// there are code revisions where the message body is not actually a zip file but rather a series of
	// <length><protobuf> messages. Fixed as of 5.2.0 P4.

	// assume that if zip.NewReader can't recognize the message body as a proper zip file, the body is
	// a protobuf bundle instead.

	if err != nil {
		return pbm.ProcessProtobufBundle(routingKey, body, headers)
	}

	for i, zf := range zipReader.File {
		src, err := zf.Open()
		if err != nil {
			log.Debugf("Error opening raw sensor event zip file content: %s. Continuing.", err.Error())
			continue
		}

		unzippedFile, err := ioutil.ReadAll(src)
		_ = src.Close()
		if err != nil {
			log.Debugf("Error opening raw sensor event file id %d from package: %s", i, err.Error())
			continue
		}

		newMsgs, err := pbm.ProcessProtobufBundle(routingKey, unzippedFile, headers)
		if err != nil {
			log.Debugf("Error processing zip filename %s: %s", zf.Name, err.Error())

			if pbm.Config.DebugFlag {
				// using anonymous func to encapsulate the defers
				func() {
					debugZip, err := zf.Open()
					if err != nil {
						log.Debugf("Error opening zip file %s for debugstore", zf.Name)
					}

					debugUnzipped, err := ioutil.ReadAll(debugZip)
					if err != nil {
						log.Debugf("Error in ioutil.ReadAll for zip file %s", zf.Name)
					}

					defer debugZip.Close()

					log.Debugf("Attempting to create file: %s", filepath.Join(pbm.Config.DebugStore, zf.Name))
					debugStoreFile, err := os.Create(filepath.Join(pbm.Config.DebugStore, zf.Name))
					if err != nil {
						log.Debugf("Error in create file %s", filepath.Join(pbm.Config.DebugStore, zf.Name))
					}

					defer debugStoreFile.Close()

					_, _ = debugStoreFile.Write(debugUnzipped)
				}()
			}
		}
		msgs = append(msgs, newMsgs...)
	}
	return msgs, nil
}

func (pbm ProtobufMessageProcessor) ProcessProtobufMessage(routingKey string, body []byte, headers amqp.Table) ([]byte, error) {
	env, err := CreateEnvMessage(headers)
	if err != nil {
		return nil, err
	}

	return pbm.ProcessProtobufMessageWithEnv(routingKey, body, headers, env)
}

func (pbm ProtobufMessageProcessor) ProcessProtobufMessageWithEnv(routingKey string, body []byte, headers amqp.Table, env *CbEnvironmentMsg) ([]byte, error) {
	cbMessage := CbEventMsg{}
	err := proto.Unmarshal(body, &cbMessage)
	if err != nil {
		return nil, err
	}

	if !pbm.isEventEnabled(&cbMessage) {
		return nil, nil
	}

	if cbMessage.Env == nil {
		cbMessage.Env = env
	}

	message, err := pbm.fromProtobufMessage(&cbMessage, routingKey)
	if err == nil {
		return message.getAsJson()
	} else {
		return nil, err
	}
}

func regmodAction(a CbRegModMsg_CbRegModAction) string {
	switch a {
	case CbRegModMsg_actionRegModCreateKey:
		return "createkey"
	case CbRegModMsg_actionRegModWriteValue:
		return "writeval"
	case CbRegModMsg_actionRegModDeleteKey:
		return "delkey"
	case CbRegModMsg_actionRegModDeleteValue:
		return "delval"
	}
	return fmt.Sprintf("unknown (%d)", int32(a))
}

func tamperAlertType(a CbTamperAlertMsg_CbTamperAlertType) string {
	switch a {
	case CbTamperAlertMsg_AlertCoreDriverUnloaded:
		return "CoreDriverUnloaded"
	case CbTamperAlertMsg_AlertNetworkDriverUnloaded:
		return "NetworkDriverUnloaded"
	case CbTamperAlertMsg_AlertCbServiceStopped:
		return "CbServiceStopped"
	case CbTamperAlertMsg_AlertCbProcessTerminated:
		return "CbProcessTerminated"
	case CbTamperAlertMsg_AlertCbCodeInjection:
		return "CbCodeInjection"
	}
	return fmt.Sprintf("unknown (%d)", int32(a))
}

func blockedProcessEventType(a CbProcessBlockedMsg_BlockEvent) string {
	switch a {
	case CbProcessBlockedMsg_ProcessCreate:
		return "ProcessCreate"
	case CbProcessBlockedMsg_RunningProcess:
		return "RunningProcess"
	}
	return fmt.Sprintf("unknown (%d)", int32(a))
}

func blockedProcessResult(a CbProcessBlockedMsg_BlockResult) string {
	switch a {
	case CbProcessBlockedMsg_ProcessTerminated:
		return "ProcessTerminated"
	case CbProcessBlockedMsg_NotTerminatedCBProcess:
		return "NotTerminatedCBProcess"
	case CbProcessBlockedMsg_NotTerminatedSystemProcess:
		return "NotTerminatedSystemProcess"
	case CbProcessBlockedMsg_NotTerminatedCriticalSystemProcess:
		return "NotTerminatedCriticalSystemProcess"
	case CbProcessBlockedMsg_NotTerminatedWhitelistedPath:
		return "NotTerminatedWhitelistPath"
	case CbProcessBlockedMsg_NotTerminatedOpenProcessError:
		return "NotTerminatedOpenProcessError"
	case CbProcessBlockedMsg_NotTerminatedTerminateError:
		return "NotTerminatedTerminateError"
	}
	return fmt.Sprintf("unknown (%d)", int32(a))
}

func emetMitigationType(a *CbEmetMitigationAction) string {

	mitigation := a.GetMitigationType()

	switch mitigation {
	case CbEmetMitigationAction_actionDep:
		return "Dep"
	case CbEmetMitigationAction_actionSehop:
		return "Sehop"
	case CbEmetMitigationAction_actionAsr:
		return "Asr"
	case CbEmetMitigationAction_actionAslr:
		return "Aslr"
	case CbEmetMitigationAction_actionNullPage:
		return "NullPage"
	case CbEmetMitigationAction_actionHeapSpray:
		return "HeapSpray"
	case CbEmetMitigationAction_actionMandatoryAslr:
		return "MandatoryAslr"
	case CbEmetMitigationAction_actionEaf:
		return "Eaf"
	case CbEmetMitigationAction_actionEafPlus:
		return "EafPlus"
	case CbEmetMitigationAction_actionBottomUpAslr:
		return "BottomUpAslr"
	case CbEmetMitigationAction_actionLoadLibrary:
		return "LoadLibrary"
	case CbEmetMitigationAction_actionMemoryProtection:
		return "MemoryProtection"
	case CbEmetMitigationAction_actionSimulateExecFlow:
		return "SimulateExecFlow"
	case CbEmetMitigationAction_actionStackPivot:
		return "StackPivot"
	case CbEmetMitigationAction_actionCallerChecks:
		return "CallerChecks"
	case CbEmetMitigationAction_actionBannedFunctions:
		return "BannedFunctions"
	case CbEmetMitigationAction_actionDeepHooks:
		return "DeepHooks"
	case CbEmetMitigationAction_actionAntiDetours:
		return "AntiDetours"
	}
	return fmt.Sprintf("unknown (%d)", int32(mitigation))
}

func crossprocOpenType(a CbCrossProcessOpenMsg_OpenType) string {
	switch a {
	case CbCrossProcessOpenMsg_OpenProcessHandle:
		return "open_process"
	case CbCrossProcessOpenMsg_OpenThreadHandle:
		return "open_thread"
	}
	return fmt.Sprintf("unknown (%d)", int32(a))
}

func filemodAction(a CbFileModMsg_CbFileModAction) string {
	switch a {
	case CbFileModMsg_actionFileModCreate:
		return "create"
	case CbFileModMsg_actionFileModWrite:
		return "write"
	case CbFileModMsg_actionFileModDelete:
		return "delete"
	case CbFileModMsg_actionFileModLastWrite:
		return "lastwrite"
	}
	return fmt.Sprintf("unknown (%d)", int32(a))
}
