package pbmessageprocessor

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"github.com/carbonblack/cb-event-forwarder/internal/sensor_events"
	"github.com/carbonblack/cb-event-forwarder/internal/util"
	"github.com/golang/protobuf/proto"
	log "github.com/sirupsen/logrus"
	"github.com/streadway/amqp"
	"io/ioutil"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
)

type PbMessageProcessor struct {
	DebugFlag   bool
	DebugStore  string
	CbServerURL string
	EventMap    map[string]interface{}
}

func GetProcessGUID(m *sensor_events.CbEventMsg) string {
	if m.Header.ProcessPid != nil && m.Header.ProcessCreateTime != nil && m.Env != nil &&
		m.Env.Endpoint != nil && m.Env.Endpoint.SensorId != nil {
		pid := m.Header.GetProcessPid()
		createTime := m.Header.GetProcessCreateTime()
		SensorId := m.Env.Endpoint.GetSensorId()

		return util.MakeGUID(SensorId, pid, createTime)
	}
	return fmt.Sprintf("%d", m.Header.GetProcessGuid())
}

type ConvertedCbMessage struct {
	OriginalMessage *sensor_events.CbEventMsg
}

func (inmsg *ConvertedCbMessage) getStringByGUID(guid int64) (string, error) {
	for _, rawString := range inmsg.OriginalMessage.GetStrings() {
		if rawString.GetGuid() == guid {
			return util.GetUnicodeFromUTF8(rawString.GetUtf8String()), nil
		}
	}
	return "", fmt.Errorf("Could not find string for id %d", guid)
}

func (pb *PbMessageProcessor) ProcessProtobufBundle(routingKey string, body []byte, headers amqp.Table) ([]map[string]interface{}, error) {
	msgs := make([]map[string]interface{}, 0, 1)
	var err error

	err = nil
	totalLength := uint64(len(body))
	i := 0

	if totalLength < 4 {
		err = fmt.Errorf("Error in ProcessProtobufBundle: body length is < 4 bytes. Giving up")
		return msgs, err
	}

	var bytesRead uint64
	for bytesRead = 0; bytesRead+4 < totalLength; {
		messageLength := (uint64)(binary.LittleEndian.Uint32(body[bytesRead : bytesRead+4]))
		bytesRead += 4

		if messageLength+bytesRead > totalLength {
			err = fmt.Errorf("Error in ProcessProtobufBundle for event index %d: Length %d is insane. Giving up",
				i, messageLength)
			break
		}

		msg, err := pb.ProcessProtobufMessage(routingKey, body[bytesRead:bytesRead+messageLength], headers)
		if err != nil {
			log.Infof("Error in ProcessProtobufBundle for event index %d: %s. Continuing to next message", i, err.Error())
		} else if msg != nil {
			msgs = append(msgs, msg)
		}

		bytesRead += messageLength
		i++
	}

	if err != nil && bytesRead < totalLength {
		err = fmt.Errorf("Error in ProcessProtobufBundle: messages did not fill entire bundle; %d bytes left",
			totalLength-bytesRead)
	}

	return msgs, err
}

func (pb *PbMessageProcessor) ProcessRawZipBundle(routingKey string, body []byte, headers amqp.Table) ([]map[string]interface{}, error) {
	msgs := make([]map[string]interface{}, 0, 1)

	bodyReader := bytes.NewReader(body)
	zipReader, err := zip.NewReader(bodyReader, (int64)(len(body)))

	// there are code revisions where the message body is not actually a zip file but rather a series of
	// <length><protobuf> messages. Fixed as of 5.2.0 P4.

	// assume that if zip.NewReader can't recognize the message body as a proper zip file, the body is
	// a protobuf bundle instead.

	if err != nil {
		return pb.ProcessProtobufBundle(routingKey, body, headers)
	}

	for i, zf := range zipReader.File {
		src, err := zf.Open()
		if err != nil {
			log.Errorf("Error opening raw sensor event zip file content: %s. Continuing.", err.Error())
			continue
		}

		unzippedFile, err := ioutil.ReadAll(src)
		src.Close()
		if err != nil {
			log.Errorf("Error opening raw sensor event file id %d from package: %s", i, err.Error())
			continue
		}

		newMsgs, err := pb.ProcessProtobufBundle(routingKey, unzippedFile, headers)
		if err != nil {

			log.Errorf("Error processing zip filename %s: %s", zf.Name, err.Error())

			if pb.DebugFlag {
				debugZip, err := zf.Open()
				if err != nil {
					log.Errorf("Error opening zip file %s for debugstore", zf.Name)
				}

				debugUnzipped, err := ioutil.ReadAll(debugZip)
				if err != nil {
					log.Errorf("Error in ioutil.ReadAll for zip file %s", zf.Name)
				}

				defer debugZip.Close()

				log.Debugf("Attempting to create file: %s", filepath.Join(pb.DebugStore, zf.Name))
				debugStoreFile, err := os.Create(filepath.Join(pb.DebugStore, zf.Name))
				if err != nil {
					log.Errorf("Error in create file %s", filepath.Join(pb.DebugStore, zf.Name))
				}

				defer debugStoreFile.Close()

				debugStoreFile.Write(debugUnzipped)
			}
		}
		msgs = append(msgs, newMsgs...)
	}
	return msgs, nil
}

func parseIntFromHeader(src interface{}) (int64, error) {
	if src == nil {
		return 0, errors.New("Nil value")
	}

	val := reflect.ValueOf(src)
	switch val.Kind() {
	case reflect.Int64:
		return src.(int64), nil
	case reflect.Int32:
		return int64(src.(int32)), nil
	case reflect.Int16:
		return int64(src.(int16)), nil
	case reflect.String:
		return strconv.ParseInt(src.(string), 10, 64)
	default:
		return 0, errors.New("unknown type")
	}
}

// TODO: This is currently called for *every* protobuf message in a bundle. This should be called only once *per bundle*.
func CreateEnvMessage(headers amqp.Table) (*sensor_events.CbEnvironmentMsg, error) {
	endpointMsg := &sensor_events.CbEndpointEnvironmentMsg{}
	if hostID, ok := headers["hostId"]; ok {
		val, err := parseIntFromHeader(hostID)
		if err != nil {
			return nil, err
		}
		endpointMsg.HostId = &val
	}
	if hostName, ok := headers["sensorHostName"]; ok {
		val, ok := hostName.(string)
		if !ok {
			return nil, errors.New("Could not parse sensor host name from message header")
		}
		endpointMsg.SensorHostName = &val
	}
	if sensorID, ok := headers["sensorId"]; ok {
		val, err := parseIntFromHeader(sensorID)
		if err != nil {
			return nil, errors.New("Could not parse sensorID from message header")
		}
		sensorID := int32(val)
		endpointMsg.SensorId = &sensorID
	}

	serverMsg := &sensor_events.CbServerEnvironmentMsg{}
	if nodeID, ok := headers["nodeId"]; ok {
		val, err := parseIntFromHeader(nodeID)
		if err != nil {
			return nil, err
		}
		nodeID := int32(val)
		serverMsg.NodeId = &nodeID
	}

	return &sensor_events.CbEnvironmentMsg{
		Endpoint: endpointMsg,
		Server:   serverMsg,
	}, nil
}

func (pb *PbMessageProcessor) ProcessProtobufMessage(routingKey string, body []byte, headers amqp.Table) (map[string]interface{}, error) {
	cbMessage := new(sensor_events.CbEventMsg)
	err := proto.Unmarshal(body, cbMessage)
	if err != nil {
		return nil, err
	}

	if cbMessage.Env == nil {
		// if the Env is nil, try to fill it in using the headers from the AMQP message
		// (the raw sensor exchange does not fill in the SensorEnv or ServerEnv messages)
		cbMessage.Env, err = CreateEnvMessage(headers)
		if err != nil {
			return nil, err
		}
	}

	inmsg := &ConvertedCbMessage{
		OriginalMessage: cbMessage,
	}

	outmsg := make(map[string]interface{})
	outmsg["event_timestamp"] = util.WindowsTimeToUnixTimeFloat(inmsg.OriginalMessage.Header.GetTimestamp())
	outmsg["process_create_time"] = util.WindowsTimeToUnixTimeFloat(inmsg.OriginalMessage.Header.GetProcessCreateTime())
	outmsg["type"] = routingKey

	outmsg["sensor_id"] = cbMessage.Env.Endpoint.GetSensorId()
	outmsg["hostname"] = cbMessage.Env.Endpoint.GetSensorHostName()

	// is the message from an endpoint event process?
	eventMsg := true

	// select only one of network or networkv2
	gotNetworkV2Message := false
	gotNetblockV2Message := false

	switch {
	case cbMessage.Process != nil:
		if _, ok := pb.EventMap["ingress.event.process"]; ok {
			pb.WriteProcessMessage(inmsg, outmsg)
		} else if _, ok := pb.EventMap["ingress.event.procstart"]; ok {
			pb.WriteProcessMessage(inmsg, outmsg)
		} else if _, ok := pb.EventMap["ingress.event.procend"]; ok {
			pb.WriteProcessMessage(inmsg, outmsg)
		} else {
			return nil, nil
		}
	case cbMessage.Modload != nil:
		if _, ok := pb.EventMap["ingress.event.moduleload"]; ok {
			pb.WriteModloadMessage(inmsg, outmsg)
		} else {
			return nil, nil
		}
	case cbMessage.Filemod != nil:
		if _, ok := pb.EventMap["ingress.event.filemod"]; ok {
			pb.WriteFilemodMessage(inmsg, outmsg)
		} else {
			return nil, nil
		}

	case cbMessage.Networkv2 != nil:
		gotNetworkV2Message = true
		if _, ok := pb.EventMap["ingress.event.netconn"]; ok {
			pb.WriteNetconn2Message(inmsg, outmsg)
		} else {
			return nil, nil
		}
	case cbMessage.Network != nil && !gotNetworkV2Message:
		if _, ok := pb.EventMap["ingress.event.netconn"]; ok {
			pb.WriteNetconnMessage(inmsg, outmsg)
		} else {
			return nil, nil
		}
	case cbMessage.Regmod != nil:
		if _, ok := pb.EventMap["ingress.event.regmod"]; ok {
			pb.WriteRegmodMessage(inmsg, outmsg)
		} else {
			return nil, nil
		}
	case cbMessage.Childproc != nil:
		if _, ok := pb.EventMap["ingress.event.childproc"]; ok {
			pb.WriteChildprocMessage(inmsg, outmsg)
		} else {
			return nil, nil
		}
	case cbMessage.Crossproc != nil:
		if _, ok := pb.EventMap["ingress.event.crossprocopen"]; ok {
			pb.WriteCrossProcMessage(inmsg, outmsg)
		} else {
			return nil, nil
		}
	case cbMessage.Emet != nil:
		if _, ok := pb.EventMap["ingress.event.emetmitigation"]; ok {
			pb.WriteEmetEvent(inmsg, outmsg)
		} else {
			return nil, nil
		}
	case cbMessage.NetconnBlockedv2 != nil:
		gotNetblockV2Message = true
		pb.WriteNetconn2BlockMessage(inmsg, outmsg)
	case cbMessage.NetconnBlocked != nil && !gotNetblockV2Message:
		pb.WriteNetconnBlockedMessage(inmsg, outmsg)
	case cbMessage.TamperAlert != nil:
		if _, ok := pb.EventMap["ingress.event.tamper"]; ok {
			eventMsg = false
			pb.WriteTamperAlertMsg(inmsg, outmsg)
		} else {
			return nil, nil
		}
	case cbMessage.Blocked != nil:
		if _, ok := pb.EventMap["ingress.event.processblock"]; ok {
			eventMsg = false
			pb.WriteProcessBlockedMsg(inmsg, outmsg)
		} else {
			return nil, nil
		}
	case cbMessage.Module != nil:
		if _, ok := pb.EventMap["ingress.event.module"]; ok {
			eventMsg = false
			pb.WriteModinfoMessage(inmsg, outmsg)
		} else {
			return nil, nil
		}
	default:
		// we ignore event types we don't understand yet.
		return nil, nil
	}

	// write metadata about the process in case this message is generated by a process on an endpoint
	if eventMsg {
		processGUID := GetProcessGUID(cbMessage)
		outmsg["process_guid"] = processGUID
		outmsg["pid"] = inmsg.OriginalMessage.Header.GetProcessPid()

		if inmsg.OriginalMessage.Header.ForkPid != nil {
			outmsg["fork_pid"] = inmsg.OriginalMessage.Header.GetForkPid()
		}

		/*
		 * Sometimes Process path is empty
		 */
		if inmsg.OriginalMessage.Header.GetProcessPath() != "" {
			outmsg["process_path"] = inmsg.OriginalMessage.Header.GetProcessPath()
		}
		if _, ok := outmsg["md5"]; !ok {
			outmsg["md5"] = util.GetMd5Hexdigest(inmsg.OriginalMessage.Header.GetProcessMd5())
		}
		if _, ok := outmsg["sha256"]; !ok {
			outmsg["sha256"] = util.GetSha256Hexdigest(inmsg.OriginalMessage.Header.GetProcessSha256())
		}

		// add link to process in the Cb UI if the Cb hostname is set
		// TODO: not happy about reaching in to the "config" object for this
		if pb.CbServerURL != "" {

			outmsg["link_process"] = util.FastStringConcat(
				pb.CbServerURL, "#analyze/", processGUID, "/0")

			outmsg["link_sensor"] = util.FastStringConcat(
				pb.CbServerURL, "#/host/", strconv.Itoa(int(cbMessage.Env.Endpoint.GetSensorId())))
		}
	}
	if len(outmsg) > 0 {
		return outmsg, nil
	} else {
		return nil, errors.New("Unable to process protobuf")
	}
}

func (pb *PbMessageProcessor) WriteProcessMessage(message *ConvertedCbMessage, kv map[string]interface{}) {
	kv["event_type"] = "proc"

	filePath, _ := message.getStringByGUID(message.OriginalMessage.Header.GetFilepathStringGuid())
	kv["path"] = filePath

	// hack to rewrite the "type" since the Cb server may make incoming process events "ingress.event.process" or
	// "ingress.event.procstart"

	if message.OriginalMessage.Process.GetCreated() {
		kv["type"] = "ingress.event.procstart"
		if message.OriginalMessage.Process.Md5Hash != nil {
			kv["md5"] = util.GetMd5Hexdigest(message.OriginalMessage.Process.GetMd5Hash())
		}
		if message.OriginalMessage.Process.Sha256Hash != nil {
			kv["sha256"] = util.GetSha256Hexdigest(message.OriginalMessage.Process.GetSha256Hash())
		}
	} else {
		kv["type"] = "ingress.event.procend"
	}

	kv["cmdline"] = util.GetUnicodeFromUTF8(message.OriginalMessage.Process.GetCommandline())

	om := message.OriginalMessage

	kv["parent_path"] = om.Process.GetParentPath()
	kv["parent_pid"] = om.Process.GetParentPid()
	kv["parent_guid"] = om.Process.GetParentGuid()
	kv["parent_create_time"] = util.WindowsTimeToUnixTimeFloat(om.Process.GetParentCreateTime())
	kv["filtering_known_dlls"] = om.Process.GetBFilteringKnownDlls()

	if message.OriginalMessage.Process.ParentMd5 != nil {
		kv["parent_md5"] = util.GetMd5Hexdigest(om.Process.GetParentMd5())
	}

	if message.OriginalMessage.Process.ParentSha256 != nil {
		kv["parent_sha256"] = util.GetSha256Hexdigest(om.Process.GetSha256Hash())
	}

	kv["expect_followon_w_md5"] = om.Process.GetExpectFollowonWMd5()

	if om.Env != nil && om.Env.Endpoint != nil && om.Env.Endpoint.SensorId != nil && om.Process.ParentPid != nil &&
		om.Process.ParentCreateTime != nil {
		kv["parent_process_guid"] = util.MakeGUID(om.Env.Endpoint.GetSensorId(), om.Process.GetParentPid(),
			om.Process.GetParentCreateTime())
	} else {
		kv["parent_process_guid"] = fmt.Sprintf("%d", om.Process.GetParentGuid())
	}

	// add link to process in the Cb UI if the Cb hostname is set
	if pb.CbServerURL != "" {
		kv["link_parent"] = fmt.Sprintf("%s#analyze/%s/0", pb.CbServerURL, kv["parent_process_guid"])
	}

	if message.OriginalMessage.Process.Username != nil {
		kv["username"] = message.OriginalMessage.Process.GetUsername()
	}

	if message.OriginalMessage.Process.Uid != nil {
		kv["uid"] = message.OriginalMessage.Process.GetUid()
	}

}

func (pb *PbMessageProcessor) WriteModloadMessage(message *ConvertedCbMessage, kv map[string]interface{}) {
	kv["event_type"] = "modload"
	kv["type"] = "ingress.event.moduleload"

	filePath, _ := message.getStringByGUID(message.OriginalMessage.Header.GetFilepathStringGuid())
	kv["path"] = filePath
	kv["md5"] = util.GetMd5Hexdigest(message.OriginalMessage.Modload.GetMd5Hash())
	kv["sha256"] = util.GetSha256Hexdigest(message.OriginalMessage.Modload.GetSha256Hash())

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

func (pb *PbMessageProcessor) WriteFilemodMessage(message *ConvertedCbMessage, kv map[string]interface{}) {
	kv["event_type"] = "filemod"
	kv["type"] = "ingress.event.filemod"

	filePath, _ := message.getStringByGUID(message.OriginalMessage.Header.GetFilepathStringGuid())
	kv["path"] = filePath

	action := message.OriginalMessage.Filemod.GetAction()
	kv["action"] = filemodAction(action)
	kv["actiontype"] = int32(action)

	fileType := message.OriginalMessage.Filemod.GetType()
	kv["filetype"] = int32(fileType)
	kv["filetype_name"] = strings.TrimPrefix(sensor_events.CbFileModMsg_CbFileType_name[int32(fileType)], "filetype")

	if message.OriginalMessage.Filemod.Md5Hash != nil {
		kv["file_md5"] = util.GetMd5Hexdigest(message.OriginalMessage.Filemod.GetMd5Hash())
	}
	if message.OriginalMessage.Filemod.Sha256Hash != nil {
		kv["file_sha256"] = util.GetSha256Hexdigest(message.OriginalMessage.Filemod.GetSha256Hash())
	}
	kv["tamper"] = message.OriginalMessage.Filemod.GetTamper()
	kv["tamper_sent"] = message.OriginalMessage.Filemod.GetTamperSent()
}

func (pb *PbMessageProcessor) WriteChildprocMessage(message *ConvertedCbMessage, kv map[string]interface{}) {
	kv["event_type"] = "childproc"
	kv["type"] = "ingress.event.childproc"
	om := message.OriginalMessage
	kv["created"] = om.Childproc.GetCreated()

	if om.Childproc.Pid != nil && om.Childproc.CreateTime != nil && om.Env != nil &&
		om.Env.Endpoint != nil && om.Env.Endpoint.SensorId != nil {
		pid := om.Childproc.GetPid()
		createTime := om.Childproc.GetCreateTime()
		SensorId := om.Env.Endpoint.GetSensorId()

		// for some reason, the Childproc.pid field is an int64 and not an int32 as it is in the process header
		// convert the pid to int32
		pid32 := int32(pid & 0xffffffff)

		kv["child_process_guid"] = util.MakeGUID(SensorId, pid32, createTime)
	} else {
		kv["child_process_guid"] = om.Childproc.GetChildGuid()
	}

	kv["child_pid"] = om.Childproc.GetPid()
	kv["tamper"] = om.Childproc.GetTamper()
	kv["tamper_sent"] = om.Childproc.GetTamperSent()
	kv["parent_guid"] = om.Childproc.GetParentGuid()

	// add link to process in the Cb UI if the Cb hostname is set
	if pb.CbServerURL != "" {
		kv["link_child"] = fmt.Sprintf("%s#analyze/%s/0", pb.CbServerURL, kv["child_process_guid"])
	}

	kv["path"] = om.Childproc.GetPath()
	kv["md5"] = util.GetMd5Hexdigest(om.Childproc.GetMd5Hash())
	kv["sha256"] = util.GetSha256Hexdigest(om.Childproc.GetSha256Hash())

	childProcType := message.OriginalMessage.Childproc.GetChildProcType()
	kv["childproc_type"] = strings.TrimPrefix(sensor_events.CbChildProcessMsg_CbChildProcType_name[int32(childProcType)],
		"childProc")

	// handle suppressed children
	if om.Childproc.Suppressed != nil &&
		om.Childproc.Suppressed.GetBIsSuppressed() {
		kv["child_suppressed"] = true
		kv["child_command_line"] = util.GetUnicodeFromUTF8(om.Childproc.GetCommandline())
		kv["child_username"] = om.Childproc.GetUsername()
	} else {
		kv["child_suppressed"] = false
	}
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

func (pb *PbMessageProcessor) WriteRegmodMessage(message *ConvertedCbMessage, kv map[string]interface{}) {
	kv["event_type"] = "regmod"
	kv["type"] = "ingress.event.regmod"

	kv["path"] = util.GetUnicodeFromUTF8(message.OriginalMessage.Regmod.GetUtf8Regpath())

	action := message.OriginalMessage.Regmod.GetAction()
	kv["action"] = regmodAction(action)
	kv["actiontype"] = int32(action)
	kv["tamper"] = message.OriginalMessage.Regmod.GetTamper()
	kv["tamper_sent"] = message.OriginalMessage.Regmod.GetTamperSent()
}

func (pb *PbMessageProcessor) WriteNetconnMessage(message *ConvertedCbMessage, kv map[string]interface{}) {
	kv["event_type"] = "netconn"
	kv["type"] = "ingress.event.netconn"

	kv["domain"] = util.GetUnicodeFromUTF8(message.OriginalMessage.Network.GetUtf8Netpath())
	kv["ipv4"] = util.GetIPv4Address(message.OriginalMessage.Network.GetIpv4Address())
	kv["port"] = util.Ntohs(uint16(message.OriginalMessage.Network.GetPort()))
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
		kv["remote_ip"] = util.GetIPv4Address(message.OriginalMessage.Network.GetRemoteIpAddress())
		kv["remote_port"] = util.Ntohs(uint16(message.OriginalMessage.Network.GetRemotePort()))
	}

	if message.OriginalMessage.Network.LocalIpAddress != nil {
		kv["local_ip"] = util.GetIPv4Address(message.OriginalMessage.Network.GetLocalIpAddress())
		kv["local_port"] = util.Ntohs(uint16(message.OriginalMessage.Network.GetLocalPort()))
	}
}

func (pb *PbMessageProcessor) GetIPAddress(ipAddress *sensor_events.CbIpAddr) string {
	if ipAddress.GetBIsIpv6() {
		b := make([]byte, 16)
		binary.LittleEndian.PutUint64(b[:8], ipAddress.GetIpv6High())
		binary.LittleEndian.PutUint64(b[8:], ipAddress.GetIpv6Low())

		ipString := net.IP(b).String()
		if ipAddress.Ipv6Scope != nil {
			return fmt.Sprintf("%s%%%s", ipString, ipAddress.GetIpv6Scope())
		}
		return ipString
	}
	return util.GetIPv4Address(ipAddress.GetIpv4Address())
}

func (pb *PbMessageProcessor) WriteNetconn2Message(message *ConvertedCbMessage, kv map[string]interface{}) {
	kv["event_type"] = "netconn"
	kv["type"] = "ingress.event.netconn"

	kv["domain"] = util.GetUnicodeFromUTF8(message.OriginalMessage.Networkv2.GetUtf8Netpath())
	kv["protocol"] = int32(message.OriginalMessage.Networkv2.GetProtocol())

	if message.OriginalMessage.Networkv2.GetOutbound() {
		kv["direction"] = "outbound"
	} else {
		kv["direction"] = "inbound"
	}

	// we are deprecating the "ipv4" and "port" keys here, since this message is guaranteed to have remote &
	// local ip and port numbers.

	if message.OriginalMessage.Networkv2.GetProxyConnection() {
		kv["proxy"] = true
		kv["proxy_ip"] = GetIPAddress(message.OriginalMessage.Networkv2.GetProxyIpAddress())
		kv["proxy_port"] = util.Ntohs(uint16(message.OriginalMessage.Networkv2.GetProxyPort()))
		kv["proxy_domain"] = message.OriginalMessage.Networkv2.GetProxyNetPath()
	} else {
		kv["proxy"] = false
	}

	kv["remote_ip"] = GetIPAddress(message.OriginalMessage.Networkv2.GetRemoteIpAddress())
	kv["remote_port"] = util.Ntohs(uint16(message.OriginalMessage.Networkv2.GetRemotePort()))

	kv["local_ip"] = GetIPAddress(message.OriginalMessage.Networkv2.GetLocalIpAddress())
	kv["local_port"] = util.Ntohs(uint16(message.OriginalMessage.Networkv2.GetLocalPort()))
}

func (pb *PbMessageProcessor) WriteModinfoMessage(message *ConvertedCbMessage, kv map[string]interface{}) {
	kv["event_type"] = "binary_info"
	kv["type"] = "ingress.event.module"

	kv["md5"] = strings.ToUpper(string(message.OriginalMessage.Module.GetMd5()))
	kv["sha256"] = strings.ToUpper(string(message.OriginalMessage.Module.GetSha256()))
	kv["size"] = message.OriginalMessage.Module.GetOriginalModuleLength()

	kv["utf8_copied_module_length"] = message.OriginalMessage.Module.GetCopiedModuleLength()
	kv["utf8_file_description"] = message.OriginalMessage.Module.GetUtf8_FileDescription()
	kv["utf8_company_name"] = message.OriginalMessage.Module.GetUtf8_CompanyName()
	kv["utf8_product_name"] = message.OriginalMessage.Module.GetUtf8_ProductName()
	kv["utf8_file_version"] = message.OriginalMessage.Module.GetUtf8_FileVersion()
	kv["utf8_comments"] = message.OriginalMessage.Module.GetUtf8_Comments()
	kv["utf8_legal_copyright"] = message.OriginalMessage.Module.GetUtf8_LegalCopyright()
	kv["utf8_legal_trademark"] = message.OriginalMessage.Module.GetUtf8_LegalTrademark()
	kv["utf8_internal_name"] = message.OriginalMessage.Module.GetUtf8_InternalName()
	kv["utf8_original_file_name"] = message.OriginalMessage.Module.GetUtf8_OriginalFileName()
	kv["utf8_product_description"] = message.OriginalMessage.Module.GetUtf8_ProductDescription()
	kv["utf8_product_version"] = message.OriginalMessage.Module.GetUtf8_ProductVersion()
	kv["utf8_private_build"] = message.OriginalMessage.Module.GetUtf8_PrivateBuild()
	kv["utf8_special_build"] = message.OriginalMessage.Module.GetUtf8_SpecialBuild()
	kv["icon"] = message.OriginalMessage.Module.GetIcon()
	kv["image_file_header"] = message.OriginalMessage.Module.GetImageFileHeader()
	kv["utf8_on_disk_filename"] = message.OriginalMessage.Module.GetUtf8_OnDiskFilename()

	digsig := make(map[string]interface{})
	digsig["result"] = message.OriginalMessage.Module.GetUtf8_DigSig_Result()
	digsig["publisher"] = message.OriginalMessage.Module.GetUtf8_DigSig_Publisher()
	digsig["program_name"] = message.OriginalMessage.Module.GetUtf8_DigSig_ProgramName()
	digsig["issuer_name"] = message.OriginalMessage.Module.GetUtf8_DigSig_IssuerName()
	digsig["subject_name"] = message.OriginalMessage.Module.GetUtf8_DigSig_SubjectName()
	digsig["result_code"] = message.OriginalMessage.Module.GetUtf8_DigSig_ResultCode()
	digsig["sign_time"] = message.OriginalMessage.Module.GetUtf8_DigSig_SignTime()

	kv["digsig"] = digsig
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

func (pb *PbMessageProcessor) WriteEmetEvent(message *ConvertedCbMessage, kv map[string]interface{}) {
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

func (pb *PbMessageProcessor) WriteCrossProcMessage(message *ConvertedCbMessage, kv map[string]interface{}) {
	kv["event_type"] = "cross_process"

	om := message.OriginalMessage

	kv["is_target"] = om.Crossproc.GetIsTarget()

	if message.OriginalMessage.Crossproc.Open != nil {
		open := message.OriginalMessage.Crossproc.Open
		kv["type"] = "ingress.event.crossprocopen"

		kv["cross_process_type"] = crossprocOpenType(open.GetType())

		kv["requested_access"] = open.GetRequestedAccess()
		kv["target_pid"] = open.GetTargetPid()
		kv["target_create_time"] = open.GetTargetProcCreateTime()
		kv["target_md5"] = util.GetMd5Hexdigest(open.GetTargetProcMd5())
		kv["target_sha256"] = util.GetSha256Hexdigest(open.GetTargetProcSha256())
		kv["target_path"] = open.GetTargetProcPath()

		pid32 := int32(open.GetTargetPid() & 0xffffffff)
		kv["target_process_guid"] = util.MakeGUID(om.Env.Endpoint.GetSensorId(), pid32, int64(open.GetTargetProcCreateTime()))
	} else {
		rt := message.OriginalMessage.Crossproc.Remotethread
		kv["type"] = "ingress.event.remotethread"

		kv["cross_process_type"] = "remote_thread"
		kv["target_pid"] = rt.GetRemoteProcPid()
		kv["target_create_time"] = rt.GetRemoteProcCreateTime()
		kv["target_md5"] = util.GetMd5Hexdigest(rt.GetRemoteProcMd5())
		kv["target_sha256"] = util.GetSha256Hexdigest(rt.GetRemoteProcSha256())
		kv["target_path"] = rt.GetRemoteProcPath()

		kv["target_process_guid"] = util.MakeGUID(om.Env.Endpoint.GetSensorId(), int32(rt.GetRemoteProcPid()), int64(rt.GetRemoteProcCreateTime()))
	}

	// add link to process in the Cb UI if the Cb hostname is set
	if pb.CbServerURL != "" {
		kv["link_target"] = fmt.Sprintf("%s#analyze/%s/0", pb.CbServerURL, kv["target_process_guid"])
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

func (pb *PbMessageProcessor) WriteTamperAlertMsg(message *ConvertedCbMessage, kv map[string]interface{}) {
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

func (pb *PbMessageProcessor) WriteProcessBlockedMsg(message *ConvertedCbMessage, kv map[string]interface{}) {
	block := message.OriginalMessage.Blocked
	kv["event_type"] = "blocked_process"
	kv["type"] = "ingress.event.processblock"

	if block.GetBlockedType() == sensor_events.CbProcessBlockedMsg_MD5Hash {
		kv["blocked_reason"] = "Md5Hash"
	} else {
		kv["blocked_reason"] = fmt.Sprintf("unknown (%d)", int32(block.GetBlockedType()))
	}

	kv["blocked_event"] = blockedProcessEventType(block.GetBlockedEvent())
	kv["md5"] = util.GetMd5Hexdigest(block.GetBlockedmd5Hash())
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
		kv["process_guid"] = util.MakeGUID(om.Env.Endpoint.GetSensorId(), int32(block.GetBlockedPid()), int64(block.GetBlockedProcCreateTime()))
		// add link to process in the Cb UI if the Cb hostname is set
		if pb.CbServerURL != "" {
			kv["link_target"] = fmt.Sprintf("%s#analyze/%s/0", pb.CbServerURL, kv["target_process_guid"])
		}
	}

	kv["command_line"] = block.GetBlockedCmdline()

	if block.GetBlockedEvent() == sensor_events.CbProcessBlockedMsg_ProcessCreate &&
		block.GetBlockResult() == sensor_events.CbProcessBlockedMsg_ProcessTerminated {
		kv["uid"] = block.GetBlockedUid()
		kv["username"] = block.GetBlockedUsername()
	}
}

func (pb *PbMessageProcessor) WriteNetconnBlockedMessage(message *ConvertedCbMessage, kv map[string]interface{}) {
	kv["event_type"] = "blocked_netconn"
	// TODO: need ingress event type for netconn blocks

	blocked := message.OriginalMessage.NetconnBlocked

	kv["domain"] = util.GetUnicodeFromUTF8(blocked.GetUtf8Netpath())
	kv["ipv4"] = util.GetIPv4Address(blocked.GetIpv4Address())
	kv["port"] = util.Ntohs(uint16(blocked.GetPort()))
	kv["protocol"] = int32(blocked.GetProtocol())

	if blocked.GetOutbound() {
		kv["direction"] = "outbound"
	} else {
		kv["direction"] = "inbound"
	}
	if blocked.RemoteIpAddress != nil {
		kv["remote_ip"] = util.GetIPv4Address(blocked.GetRemoteIpAddress())
		kv["remote_port"] = util.Ntohs(uint16(blocked.GetRemotePort()))
	}

	if blocked.LocalIpAddress != nil {
		kv["local_ip"] = util.GetIPv4Address(blocked.GetLocalIpAddress())
		kv["local_port"] = util.Ntohs(uint16(blocked.GetLocalPort()))
	}
}

func (pb *PbMessageProcessor) WriteNetconn2BlockMessage(message *ConvertedCbMessage, kv map[string]interface{}) {
	kv["event_type"] = "blocked_netconn"
	// TODO: need ingress event type for netconn blocks

	blocked := message.OriginalMessage.NetconnBlockedv2

	kv["domain"] = util.GetUnicodeFromUTF8(blocked.GetUtf8Netpath())
	// we are deprecating the "ipv4" and "port" keys here, since this message is guaranteed to have remote &
	// local ip and port numbers.

	kv["protocol"] = int32(blocked.GetProtocol())

	if blocked.GetOutbound() {
		kv["direction"] = "outbound"
	} else {
		kv["direction"] = "inbound"
	}
	kv["remote_ip"] = GetIPAddress(blocked.GetRemoteIpAddress())
	kv["remote_port"] = util.Ntohs(uint16(blocked.GetRemotePort()))

	kv["local_ip"] = GetIPAddress(blocked.GetLocalIpAddress())
	kv["local_port"] = util.Ntohs(uint16(blocked.GetLocalPort()))

	if blocked.GetProxyConnection() {
		kv["proxy"] = true
		kv["proxy_ip"] = GetIPAddress(blocked.GetProxyIpAddress())
		kv["proxy_port"] = util.Ntohs(uint16(blocked.GetProxyPort()))
		kv["proxy_domain"] = blocked.GetProxyNetPath()
	} else {
		kv["proxy"] = false
	}
}

func GetIPAddress(ipAddress *sensor_events.CbIpAddr) string {
	if ipAddress.GetBIsIpv6() {
		b := make([]byte, 16)
		binary.LittleEndian.PutUint64(b[:8], ipAddress.GetIpv6High())
		binary.LittleEndian.PutUint64(b[8:], ipAddress.GetIpv6Low())

		ipString := net.IP(b).String()
		if ipAddress.Ipv6Scope != nil {
			return fmt.Sprintf("%s%%%s", ipString, ipAddress.GetIpv6Scope())
		}
		return ipString
	}
	return util.GetIPv4Address(ipAddress.GetIpv4Address())
}

func (pbm *PbMessageProcessor) ProcessProtobuf(routingKey string, indata []byte) ([]map[string]interface{}, error) {
	emptyHeaders := new(amqp.Table)

	msg, err := pbm.ProcessProtobufMessage(routingKey, indata, *emptyHeaders)
	if err != nil {
		return nil, err
	}
	msgs := make([]map[string]interface{}, 0, 0)
	msgs = append(msgs, msg)
	return msgs, nil
}
