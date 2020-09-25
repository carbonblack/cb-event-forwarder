package main

import (
    "archive/zip"
    "bytes"
    "encoding/binary"
    "errors"
    "fmt"
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

func getProcessGUID(m *CbEventMsg) string {
    if m.Header.ProcessPid != nil && m.Header.ProcessCreateTime != nil && m.Env != nil && m.Env.Endpoint != nil &&
    		m.Env.Endpoint.SensorId != nil {
        pid := m.Header.GetProcessPid()
        createTime := m.Header.GetProcessCreateTime()
        sensorID := m.Env.Endpoint.GetSensorId()

        return MakeGUID(sensorID, pid, createTime)
    }
    return fmt.Sprintf("%d", m.Header.GetProcessGuid())
}

func getParentProcessGUID(m *CbEventMsg) string {
    if m.Header.ProcessPid != nil && m.Header.ProcessCreateTime != nil && m.Env != nil && m.Env.Endpoint != nil &&
    		m.Env.Endpoint.SensorId != nil {
        pid := m.Header.GetProcessPid()
        createTime := m.Header.GetProcessCreateTime()
        sensorID := m.Env.Endpoint.GetSensorId()

        return MakeGUID(sensorID, pid, createTime)
    }
    return fmt.Sprintf("%d", m.Header.GetProcessGuid())
}

type convertedCbMessage struct {
    OriginalMessage *CbEventMsg
}

func (inmsg *convertedCbMessage) getStringByGUID(guid int64) (string, error) {
    for _, rawString := range inmsg.OriginalMessage.GetStrings() {
        if rawString.GetGuid() == guid {
            return GetUnicodeFromUTF8(rawString.GetUtf8String()), nil
        }
    }
    return "", fmt.Errorf("could not find string for id %d", guid)
}

func ProcessProtobufBundle(routingKey string, body []byte, headers amqp.Table) ([]map[string]interface{}, error) {
    msgs := make([]map[string]interface{}, 0, 1)
    var err error

    err = nil
    totalLength := uint64(len(body))
    i := 0

    if totalLength < 4 {
        err = fmt.Errorf("error in ProcessProtobufBundle: body length is < 4 bytes. Giving up")
        return msgs, err
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

        msg, err := ProcessProtobufMessage(routingKey, body[bytesRead:bytesRead+messageLength], headers)
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
            log.Debugf("Error opening raw sensor event zip file content: %s. Continuing.", err.Error())
            continue
        }

        unzippedFile, err := ioutil.ReadAll(src)
		_ = src.Close()
        if err != nil {
            log.Debugf("Error opening raw sensor event file id %d from package: %s", i, err.Error())
            continue
        }

        newMsgs, err := ProcessProtobufBundle(routingKey, unzippedFile, headers)
        if err != nil {
            log.Errorf("Error processing zip filename %s: %s", zf.Name, err.Error())

            if config.DebugFlag {
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

                    log.Debugf("Attempting to create file: %s", filepath.Join(config.DebugStore, zf.Name))
                    debugStoreFile, err := os.Create(filepath.Join(config.DebugStore, zf.Name))
                    if err != nil {
                        log.Debugf("Error in create file %s", filepath.Join(config.DebugStore, zf.Name))
                    }

                    //noinspection GoNilness
                    defer debugStoreFile.Close()

                    //noinspection GoNilness
                    _, _ = debugStoreFile.Write(debugUnzipped)
                } ()
            }
        }
        msgs = append(msgs, newMsgs...)
    }
    return msgs, nil
}

func parseIntFromHeader(src interface{}) (int64, error) {
    if src == nil {
        return 0, errors.New("nil value")
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
func createEnvMessage(headers amqp.Table) (*CbEnvironmentMsg, error) {
    endpointMsg := &CbEndpointEnvironmentMsg{}
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
            return nil, errors.New("could not parse sensor host name from message header")
        }
        endpointMsg.SensorHostName = &val
    }
    if sensorID, ok := headers["sensorId"]; ok {
        val, err := parseIntFromHeader(sensorID)
        if err != nil {
            return nil, errors.New("could not parse sensorID from message header")
        }
        sensorID := int32(val)
        endpointMsg.SensorId = &sensorID
    }

    serverMsg := &CbServerEnvironmentMsg{}
    if nodeID, ok := headers["nodeId"]; ok {
        val, err := parseIntFromHeader(nodeID)
        if err != nil {
            return nil, err
        }
        nodeID := int32(val)
        serverMsg.NodeId = &nodeID
    }

    return &CbEnvironmentMsg{
        Endpoint: endpointMsg,
        Server:   serverMsg,
    }, nil
}

func ProcessProtobufMessage(routingKey string, body []byte, headers amqp.Table) (map[string]interface{}, error) {
    cbMessage := new(CbEventMsg)
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

    inmsg := &convertedCbMessage{
        OriginalMessage: cbMessage,
    }

    outmsg := make(map[string]interface{})
    if config.UseTimeFloat {
        outmsg["timestamp"] = WindowsTimeToUnixTimeFloat(inmsg.OriginalMessage.Header.GetTimestamp())
    } else {
        outmsg["timestamp"] = WindowsTimeToUnixTime(inmsg.OriginalMessage.Header.GetTimestamp())
    }
    outmsg["type"] = routingKey
    outmsg["sensor_id"] = cbMessage.Env.Endpoint.GetSensorId()
    outmsg["computer_name"] = cbMessage.Env.Endpoint.GetSensorHostName()

    // is the message from an endpoint event process?
    eventMsg := true

    // select only one of network or networkv2
    gotNetworkV2Message := false

    switch {
        case cbMessage.Process != nil:
            var retVal error = nil
            if _, ok := config.EventMap["ingress.event.process"]; ok {
                retVal = writeProcessMessage(inmsg, outmsg)
            } else if _, ok := config.EventMap["ingress.event.procstart"]; ok {
                retVal = writeProcessMessage(inmsg, outmsg)
            } else if _, ok := config.EventMap["ingress.event.procend"]; ok {
                retVal = writeProcessMessage(inmsg, outmsg)
            } else {
                return nil, nil
            }
            if retVal != nil {
                return nil, nil
            }
        case cbMessage.Modload != nil:
            if _, ok := config.EventMap["ingress.event.moduleload"]; ok {
                writeModloadMessage(inmsg, outmsg)
            } else {
                return nil, nil
            }
        case cbMessage.Filemod != nil:
            if _, ok := config.EventMap["ingress.event.filemod"]; ok {
                writeFilemodMessage(inmsg, outmsg)
            } else {
                return nil, nil
            }

        case cbMessage.Networkv2 != nil:
            gotNetworkV2Message = true
            if _, ok := config.EventMap["ingress.event.netconn"]; ok {
                writeNetconn2Message(inmsg, outmsg)
            } else {
                return nil, nil
            }
        case cbMessage.Network != nil && !gotNetworkV2Message:
            if _, ok := config.EventMap["ingress.event.netconn"]; ok {
                writeNetconnMessage(inmsg, outmsg)
            } else {
                return nil, nil
            }
        case cbMessage.Regmod != nil:
            if _, ok := config.EventMap["ingress.event.regmod"]; ok {
                writeRegmodMessage(inmsg, outmsg)
            } else {
                return nil, nil
            }
        case cbMessage.Childproc != nil:
            if _, ok := config.EventMap["ingress.event.childproc"]; ok {
                writeChildprocMessage(inmsg, outmsg)
            } else {
                return nil, nil
            }
        case cbMessage.Crossproc != nil:
            if _, ok := config.EventMap["ingress.event.crossprocopen"]; ok {
                writeCrossProcMessage(inmsg, outmsg)
            } else {
                return nil, nil
            }
        case cbMessage.Emet != nil:
            if _, ok := config.EventMap["ingress.event.emetmitigation"]; ok {
                writeEmetEvent(inmsg, outmsg)
            } else {
                return nil, nil
            }
        case cbMessage.Scriptexe != nil:
            if _, ok:= config.EventMap["ingress.event.filelessscriptload"]; ok {
                writeScriptexeMsg(inmsg,outmsg)
            } else {
                return nil, nil
            }
        case cbMessage.NetconnBlockedv2 != nil:
            // generated by sensor, but not used in CbR (CB-23994)
            log.Debugf("ignoring unhandled event netconnblockedv2")
        case cbMessage.NetconnBlocked != nil:
            // generated by sensor, but not used in CbR (CB-23994)
            log.Debugf("ignoring unhandled event netconnblocked")
        case cbMessage.TamperAlert != nil:
            if _, ok := config.EventMap["ingress.event.tamper"]; ok {
                eventMsg = false
                writeTamperAlertMsg(inmsg, outmsg)
            } else {
                return nil, nil
            }
        case cbMessage.Blocked != nil:
            if _, ok := config.EventMap["ingress.event.processblock"]; ok {
                eventMsg = false
                writeProcessBlockedMsg(inmsg, outmsg)
            } else {
                return nil, nil
            }
        case cbMessage.Module != nil:
            if _, ok := config.EventMap["ingress.event.module"]; ok {
                eventMsg = false
                writeModinfoMessage(inmsg, outmsg)
            } else {
                return nil, nil
            }
        default:
            // we ignore event types we don't understand yet.
            return nil, nil
    }

    // write metadata about the process in case this message is generated by a process on an endpoint
    if eventMsg {
        processGUID := getProcessGUID(cbMessage)
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
            outmsg["md5"] = GetMd5Hexdigest(inmsg.OriginalMessage.Header.GetProcessMd5())
        }
        if _, ok := outmsg["sha256"]; !ok {
            outmsg["sha256"] = GetSha256Hexdigest(inmsg.OriginalMessage.Header.GetProcessSha256())
        }

        // add link to process in the Cb UI if the Cb hostname is set
        // TODO: not happy about reaching in to the "config" object for this
        if config.CbServerURL != "" {

            outmsg["link_process"] = FastStringConcat(
                config.CbServerURL, "#analyze/", processGUID, "/0")

            outmsg["link_sensor"] = FastStringConcat(
                config.CbServerURL, "#/host/", strconv.Itoa(int(cbMessage.Env.Endpoint.GetSensorId())))
        }
    }

    return outmsg, nil
}

func writeProcessMessage(message *convertedCbMessage, kv map[string]interface{}) error {
    kv["event_type"] = "proc"

    filePath, _ := message.getStringByGUID(message.OriginalMessage.Header.GetFilepathStringGuid())
    kv["path"] = filePath

    // hack to rewrite the "type" since the Cb server may make incoming process events "ingress.event.process" or
    // "ingress.event.procstart"

    if message.OriginalMessage.Process.GetCreated() {
        kv["type"] = "ingress.event.procstart"
        if message.OriginalMessage.Process.Md5Hash != nil {
            kv["md5"] = GetMd5Hexdigest(message.OriginalMessage.Process.GetMd5Hash())
        }
        if message.OriginalMessage.Process.Sha256Hash != nil {
            kv["sha256"] = GetSha256Hexdigest(message.OriginalMessage.Process.GetSha256Hash())
        }

    } else if _, ok := config.EventMap["ingress.event.procend"]; ok {
        /*
         * We don't want procends to be generated if procends are not in the config map
         * Since this function handles all three cases (process, procstart, procend) we need
         * to check again prior to outputting this event type
         */
        kv["type"] = "ingress.event.procend"
    } else {
        return errors.New("ingress.event.procend not specified in config map")
    }

    kv["command_line"] = GetUnicodeFromUTF8(message.OriginalMessage.Process.GetCommandline())

    om := message.OriginalMessage

    kv["parent_path"] = om.Process.GetParentPath()
    kv["parent_pid"] = om.Process.GetParentPid()
    kv["parent_guid"] = om.Process.GetParentGuid()
    kv["parent_create_time"] = WindowsTimeToUnixTimeFloat(om.Process.GetParentCreateTime())
    if config.UseTimeFloat {
        kv["parent_create_time"] = WindowsTimeToUnixTimeFloat(om.Process.GetParentCreateTime())
    } else {
        kv["parent_create_time"] = WindowsTimeToUnixTime(om.Process.GetParentCreateTime())
    }
    kv["filtering_known_dlls"] = om.Process.GetBFilteringKnownDlls()

    if message.OriginalMessage.Process.ParentMd5 != nil {
        kv["parent_md5"] = GetMd5Hexdigest(om.Process.GetParentMd5())
    }

    if message.OriginalMessage.Process.ParentSha256 != nil {
        kv["parent_sha256"] = GetSha256Hexdigest(om.Process.GetParentSha256())
    }

    kv["expect_followon_w_md5"] = om.Process.GetExpectFollowonWMd5()

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

    if message.OriginalMessage.Process.Uid != nil {
        kv["uid"] = message.OriginalMessage.Process.GetUid()
    }

    return nil
}

func writeScriptexeMsg(message *convertedCbMessage, kv map[string]interface{}) {
    kv["event_type"] = "FilelessScriptload"
    kv["type"] = "ingress.event.filelessscriptload"

    kv["script"] = message.OriginalMessage.Scriptexe.GetScript()
    kv["script_sha256"] = message.OriginalMessage.Scriptexe.GetSha256()
}

func writeModloadMessage(message *convertedCbMessage, kv map[string]interface{}) {
    kv["event_type"] = "modload"
    kv["type"] = "ingress.event.moduleload"

    filePath, _ := message.getStringByGUID(message.OriginalMessage.Header.GetFilepathStringGuid())
    kv["path"] = filePath
    kv["md5"] = GetMd5Hexdigest(message.OriginalMessage.Modload.GetMd5Hash())
    kv["sha256"] = GetSha256Hexdigest(message.OriginalMessage.Modload.GetSha256Hash())
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

func writeFilemodMessage(message *convertedCbMessage, kv map[string]interface{}) {
    kv["event_type"] = "filemod"
    kv["type"] = "ingress.event.filemod"

    filePath, _ := message.getStringByGUID(message.OriginalMessage.Header.GetFilepathStringGuid())
    kv["path"] = filePath

    action := message.OriginalMessage.Filemod.GetAction()
    kv["action"] = filemodAction(action)
    kv["actiontype"] = int32(action)

    fileType := message.OriginalMessage.Filemod.GetType()
    kv["filetype"] = int32(fileType)
    kv["filetype_name"] = strings.TrimPrefix(CbFileModMsg_CbFileType_name[int32(fileType)], "filetype")

    if message.OriginalMessage.Filemod.Md5Hash != nil {
        kv["file_md5"] = GetMd5Hexdigest(message.OriginalMessage.Filemod.GetMd5Hash())
    }
    if message.OriginalMessage.Filemod.Sha256Hash != nil {
        kv["file_sha256"] = GetSha256Hexdigest(message.OriginalMessage.Filemod.GetSha256Hash())
    }
    kv["tamper"] = message.OriginalMessage.Filemod.GetTamper()
    kv["tamper_sent"] = message.OriginalMessage.Filemod.GetTamperSent()
}

func writeChildprocMessage(message *convertedCbMessage, kv map[string]interface{}) {
    kv["event_type"] = "childproc"
    kv["type"] = "ingress.event.childproc"
    om := message.OriginalMessage
    kv["created"] = om.Childproc.GetCreated()

    if om.Childproc.Pid != nil && om.Childproc.CreateTime != nil && om.Env != nil &&
            om.Env.Endpoint != nil && om.Env.Endpoint.SensorId != nil {
        pid := om.Childproc.GetPid()
        createTime := om.Childproc.GetCreateTime()
        sensorID := om.Env.Endpoint.GetSensorId()

        // for some reason, the Childproc.pid field is an int64 and not an int32 as it is in the process header
        // convert the pid to int32
        pid32 := int32(pid & 0xffffffff)

        kv["child_process_guid"] = MakeGUID(sensorID, pid32, createTime)
    } else {
        kv["child_process_guid"] = om.Childproc.GetChildGuid()
    }

    kv["child_pid"] = om.Childproc.GetPid()
    kv["tamper"] = om.Childproc.GetTamper()
    kv["tamper_sent"] = om.Childproc.GetTamperSent()

    sensorID := om.Env.Endpoint.GetSensorId()
    if om.Header.ProcessCreateTime != nil  && om.Header.ProcessPid != nil {
        processCreateTime := om.Header.GetProcessCreateTime()
        processPid := om.Header.GetProcessPid()
        kv["parent_guid"] = MakeGUID(sensorID, processPid, processCreateTime)
    } else { 
        kv["parent_guid"] = om.Childproc.GetParentGuid()
    }

    // add link to process in the Cb UI if the Cb hostname is set
    if config.CbServerURL != "" {
        kv["link_child"] = fmt.Sprintf("%s#analyze/%s/1", config.CbServerURL, kv["child_process_guid"])
    }

    kv["path"] = om.Childproc.GetPath()
    kv["md5"] = GetMd5Hexdigest(om.Childproc.GetMd5Hash())
    kv["sha256"] = GetSha256Hexdigest(om.Childproc.GetSha256Hash())

    childProcType := message.OriginalMessage.Childproc.GetChildProcType()
    kv["childproc_type"] = strings.TrimPrefix(CbChildProcessMsg_CbChildProcType_name[int32(childProcType)],
        "childProc")

    // handle suppressed children
    if om.Childproc.Suppressed != nil &&
        om.Childproc.Suppressed.GetBIsSuppressed() {
        kv["child_suppressed"] = true
        kv["child_command_line"] = GetUnicodeFromUTF8(om.Childproc.GetCommandline())
        kv["child_username"] = om.Childproc.GetUsername()
    } else {
        kv["child_suppressed"] = false
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

func writeRegmodMessage(message *convertedCbMessage, kv map[string]interface{}) {
    kv["event_type"] = "regmod"
    kv["type"] = "ingress.event.regmod"

    kv["path"] = GetUnicodeFromUTF8(message.OriginalMessage.Regmod.GetUtf8Regpath())

    action := message.OriginalMessage.Regmod.GetAction()
    kv["action"] = regmodAction(action)
    kv["actiontype"] = int32(action)
    kv["tamper"] = message.OriginalMessage.Regmod.GetTamper()
    kv["tamper_sent"] = message.OriginalMessage.Regmod.GetTamperSent()
}

func writeNetconnMessage(message *convertedCbMessage, kv map[string]interface{}) {
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

func getIPAddress(ipAddress *CbIpAddr) string {
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
    return GetIPv4Address(ipAddress.GetIpv4Address())
}

func writeNetconn2Message(message *convertedCbMessage, kv map[string]interface{}) {
    kv["event_type"] = "netconn"
    kv["type"] = "ingress.event.netconn"

    kv["domain"] = GetUnicodeFromUTF8(message.OriginalMessage.Networkv2.GetUtf8Netpath())
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
        kv["proxy_ip"] = getIPAddress(message.OriginalMessage.Networkv2.GetProxyIpAddress())
        kv["proxy_port"] = ntohs(uint16(message.OriginalMessage.Networkv2.GetProxyPort()))
        kv["proxy_domain"] = message.OriginalMessage.Networkv2.GetProxyNetPath()
    } else {
        kv["proxy"] = false
    }

    kv["remote_ip"] = getIPAddress(message.OriginalMessage.Networkv2.GetRemoteIpAddress())
    kv["remote_port"] = ntohs(uint16(message.OriginalMessage.Networkv2.GetRemotePort()))

    kv["local_ip"] = getIPAddress(message.OriginalMessage.Networkv2.GetLocalIpAddress())
    kv["local_port"] = ntohs(uint16(message.OriginalMessage.Networkv2.GetLocalPort()))
}

func writeModinfoMessage(message *convertedCbMessage, kv map[string]interface{}) {
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

func writeEmetEvent(message *convertedCbMessage, kv map[string]interface{}) {
    kv["event_type"] = "emet_mitigation"
    kv["type"] = "ingress.event.emetmitigation"
    kv["log_message"] = message.OriginalMessage.Emet.GetActionText()
    kv["mitigation"] = emetMitigationType(message.OriginalMessage.Emet.GetAction())
    kv["blocked"] = message.OriginalMessage.Emet.GetBlocked()
    kv["log_id"] = message.OriginalMessage.Emet.GetEmetId()
    kv["emet_timestamp"] = message.OriginalMessage.Emet.GetEmetTimstamp()
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

func writeCrossProcMessage(message *convertedCbMessage, kv map[string]interface{}) {
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
        kv["target_md5"] = GetMd5Hexdigest(open.GetTargetProcMd5())
        kv["target_sha256"] = GetSha256Hexdigest(open.GetTargetProcSha256())
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
        kv["target_sha256"] = GetSha256Hexdigest(rt.GetRemoteProcSha256())
        kv["target_path"] = rt.GetRemoteProcPath()

        kv["target_process_guid"] = MakeGUID(om.Env.Endpoint.GetSensorId(), int32(rt.GetRemoteProcPid()), int64(rt.GetRemoteProcCreateTime()))
    }

    // add link to process in the Cb UI if the Cb hostname is set
    if config.CbServerURL != "" {
        kv["link_target"] = fmt.Sprintf("%s#analyze/%s/1", config.CbServerURL, kv["target_process_guid"])
    }
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

func writeTamperAlertMsg(message *convertedCbMessage, kv map[string]interface{}) {
    kv["event_type"] = "tamper"
    kv["type"] = "ingress.event.tamper"
    kv["tamper_type"] = tamperAlertType(message.OriginalMessage.TamperAlert.GetType())
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

func writeProcessBlockedMsg(message *convertedCbMessage, kv map[string]interface{}) {
    block := message.OriginalMessage.Blocked
    kv["event_type"] = "blocked_process"
    kv["type"] = "ingress.event.processblock"

    if block.GetBlockedType() == CbProcessBlockedMsg_MD5Hash {
        kv["blocked_reason"] = "Md5Hash"
    } else {
        kv["blocked_reason"] = fmt.Sprintf("unknown (%d)", int32(block.GetBlockedType()))
    }

    kv["blocked_event"] = blockedProcessEventType(block.GetBlockedEvent())
    kv["md5"] = GetMd5Hexdigest(block.GetBlockedmd5Hash())
    kv["path"] = block.GetBlockedPath()
    kv["blocked_result"] = blockedProcessResult(block.GetBlockResult())

    if block.GetBlockResult() == CbProcessBlockedMsg_NotTerminatedOpenProcessError ||
        block.GetBlockResult() == CbProcessBlockedMsg_NotTerminatedTerminateError {
        kv["blocked_error"] = block.GetBlockError()
    }

    if block.BlockedPid != nil {
        kv["pid"] = block.GetBlockedPid()
        kv["process_create_time"] = block.GetBlockedProcCreateTime()

        om := message.OriginalMessage
        kv["process_guid"] = MakeGUID(om.Env.Endpoint.GetSensorId(), block.GetBlockedPid(), int64(block.GetBlockedProcCreateTime()))
        // add link to process in the Cb UI if the Cb hostname is set
        if config.CbServerURL != "" {
            kv["link_target"] = fmt.Sprintf("%s#analyze/%s/1", config.CbServerURL, kv["target_process_guid"])
        }
    }

    kv["command_line"] = block.GetBlockedCmdline()

    if block.GetBlockedEvent() == CbProcessBlockedMsg_ProcessCreate &&
        block.GetBlockResult() == CbProcessBlockedMsg_ProcessTerminated {
        kv["uid"] = block.GetBlockedUid()
        kv["username"] = block.GetBlockedUsername()
    }
}

// for future use, if CbR decides to handle this event
//noinspection GoUnusedFunction
func writeNetconnBlockedMessage(message *convertedCbMessage, kv map[string]interface{}) {
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

// for future use, if CbR decides to handle this event
//noinspection GoUnusedFunction
func writeNetconn2BlockMessage(message *convertedCbMessage, kv map[string]interface{}) {
    kv["event_type"] = "blocked_netconn"
    // TODO: need ingress event type for netconn blocks

    blocked := message.OriginalMessage.NetconnBlockedv2

    kv["domain"] = GetUnicodeFromUTF8(blocked.GetUtf8Netpath())
    // we are deprecating the "ipv4" and "port" keys here, since this message is guaranteed to have remote &
    // local ip and port numbers.

    kv["protocol"] = int32(blocked.GetProtocol())

    if blocked.GetOutbound() {
        kv["direction"] = "outbound"
    } else {
        kv["direction"] = "inbound"
    }
    kv["remote_ip"] = getIPAddress(blocked.GetRemoteIpAddress())
    kv["remote_port"] = ntohs(uint16(blocked.GetRemotePort()))

    kv["local_ip"] = getIPAddress(blocked.GetLocalIpAddress())
    kv["local_port"] = ntohs(uint16(blocked.GetLocalPort()))

    if blocked.GetProxyConnection() {
        kv["proxy"] = true
        kv["proxy_ip"] = getIPAddress(blocked.GetProxyIpAddress())
        kv["proxy_port"] = ntohs(uint16(blocked.GetProxyPort()))
        kv["proxy_domain"] = blocked.GetProxyNetPath()
    } else {
        kv["proxy"] = false
    }
}
