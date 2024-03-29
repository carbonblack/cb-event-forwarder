package utils

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	. "github.com/carbonblack/cb-event-forwarder/pkg/sensorevents"
	log "github.com/sirupsen/logrus"
	"github.com/streadway/amqp"
	"gopkg.in/h2non/filetype.v1"
	"net"
	"os"
	"reflect"
	"strconv"
)

/*
 * conversion routines
 */
func GetProcessGUID(m *CbEventMsg) string {
	if m.Header.ProcessPid != nil && m.Header.ProcessCreateTime != nil && m.Env != nil && m.Env.Endpoint != nil &&
		m.Env.Endpoint.SensorId != nil {
		pid := m.Header.GetProcessPid()
		createTime := m.Header.GetProcessCreateTime()
		sensorID := m.Env.Endpoint.GetSensorId()

		return MakeGUID(sensorID, pid, createTime)
	}
	return fmt.Sprintf("%d", m.Header.GetProcessGuid())
}

func WindowsTimeToUnixTime(windowsTime int64) uint64 {
	// number of milliseconds between Jan 1st 1601 and Jan 1st 1970
	var timeShift uint64 = 11644473600000

	if windowsTime == 0 {
		return 0
	}

	windowstime64 := uint64(windowsTime)
	windowstime64 /= 10000     // ns to ms
	windowstime64 -= timeShift // since 1601 to since 1970
	windowstime64 /= 1000
	return windowstime64
}

func WindowsTimeToUnixTimeFloat(windowsTime int64) float64 {
	// number of milliseconds between Jan 1st 1601 and Jan 1st 1970
	var timeShift, newTime float64
	timeShift = 11644473600000
	newTime = float64(windowsTime)

	if windowsTime == 0 {
		return newTime
	}

	newTime /= 10000     // ns to ms
	newTime -= timeShift // since 1601 to since 1970
	newTime /= 1000
	return newTime
}

func MakeGUID(sensorID, pid int32, createTime int64) string {
	guidPart1 := uint32(sensorID)
	guidPart2 := uint16(pid >> 16)
	guidPart3 := uint16(pid & 0xffff)
	guidPart4 := uint16(createTime >> 48)
	guidPart5 := uint16((createTime >> 32) & 0xffff)
	guidPart6 := uint32(createTime & 0xffffffff)

	return fmt.Sprintf("%08x-%04x-%04x-%04x-%04x%08x", guidPart1, guidPart2, guidPart3, guidPart4,
		guidPart5, guidPart6)
}

func GetIPv4Address(addr uint32) string {
	buf := make([]byte, 4)
	binary.LittleEndian.PutUint32(buf, addr)
	return net.IP(buf).String()
}

/*
 * NOTE: the interface_ip and comms_ip (from JSON events) are byte-flipped from the IP addresses in the
 * sensor protobuf events.
 */
func GetIPv4AddressSigned(addr int32) string {
	buf := bytes.Buffer{}

	if err := binary.Write(&buf, binary.BigEndian, addr); err != nil {
		return "<unknown>"
	}

	b := buf.Bytes()

	return net.IPv4(b[0], b[1], b[2], b[3]).String()
}

func Ntohs(p uint16) uint16 {
	return ((p >> 8) & 0xff) | ((p << 8) & 0xff00)
}

func GetMd5Hexdigest(src []byte) string {
	if len(src) != 16 && len(src) != 0 {
		log.WithFields(log.Fields{"Md5Length": len(src), "Md5": fmt.Sprintf("%X", src)}).Debug("Invalid expected length of Md5")
	}
	return fmt.Sprintf("%X", src)
}

func GetSha256Hexdigest(src []byte) string {
	return fmt.Sprintf("%X", src)
}

func GetUnicodeFromUTF8(src []byte) string {
	return string(src)
}

func FastStringConcat(substrings ...string) string {
	/*
	 * PreAllocate for speed
	 */
	buffer := bytes.NewBuffer(make([]byte, 0, 32))

	for _, substring := range substrings {
		buffer.WriteString(substring)
	}
	return buffer.String()
}

func IsGzip(fp *os.File) bool {
	stats, statsErr := fp.Stat()
	if statsErr == nil {
		log.Debugf("File stats = %s:%d:%v", stats.Name(), stats.Size(), stats.Mode())
	}
	// decompress file from disk if it's compressed
	header := make([]byte, 261)

	fp.Seek(0, os.SEEK_SET)

	_, err := fp.Read(header)
	if err != nil {
		log.Debugf("Could not read header information for file: %s", err.Error())
		return false
	}

	fp.Seek(0, os.SEEK_SET)

	if filetype.IsMIME(header, "application/gzip") {
		return true
	}
	return false
}

func GetIPAddress(ipAddress *CbIpAddr) string {
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

func CreateEnvMessage(headers amqp.Table) (*CbEnvironmentMsg, error) {
	endpointMsg := &CbEndpointEnvironmentMsg{}
	if hostID, ok := headers["hostId"]; ok {
		val, err := ParseIntFromHeader(hostID)
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
		val, err := ParseIntFromHeader(sensorID)
		if err != nil {
			return nil, errors.New("could not parse sensorID from message header")
		}
		sensorID := int32(val)
		endpointMsg.SensorId = &sensorID
	}

	serverMsg := &CbServerEnvironmentMsg{}
	if nodeID, ok := headers["nodeId"]; ok {
		val, err := ParseIntFromHeader(nodeID)
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

func ParseIntFromHeader(src interface{}) (int64, error) {
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
