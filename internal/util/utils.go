package util

import (
	"bytes"
	"encoding/binary"
	"fmt"
	log "github.com/sirupsen/logrus"
	"gopkg.in/h2non/filetype.v1"
	"net"
	"os"
	"path"
	"path/filepath"
	"plugin"
	"text/template"
)

/*
 * conversion routines
 */

func LoadFuncMapFromPlugin(pluginPath string, pluginName string) template.FuncMap {
	log.Infof("loadPluginFuncMap: Trying to load plugin funcmap provider %s at %s", pluginName, pluginPath)
	plug, err := plugin.Open(path.Join(pluginPath, pluginName+".so"))
	if err != nil {
		log.Panic(err)
	}
	pluginGetFuncMapRaw, err := plug.Lookup("GetFuncMap")
	if err != nil {
		log.Panicf("Failed to load encoder plugin %v", err)
	}
	return pluginGetFuncMapRaw.(func() template.FuncMap)()
}

func WindowsTimeToUnixTime(windowsTime int64) int64 {
	// number of milliseconds between Jan 1st 1601 and Jan 1st 1970
	var timeShift int64
	timeShift = 11644473600000

	if windowsTime == 0 {
		return windowsTime
	}

	windowsTime /= 10000     // ns to ms
	windowsTime -= timeShift // since 1601 to since 1970
	windowsTime /= 1000
	return windowsTime
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
		//log.WithFields(log.Fields{"Md5Length": len(src), "Md5": fmt.Sprintf("%X", src)}).Debug("Invalid expected length of Md5")
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

func MoveFileToDebug(debugFlag bool, debugStore string, name string) {
	if debugFlag {
		baseName := filepath.Base(name)
		dest := filepath.Join(debugStore, baseName)
		log.Debugf("MoveFileToDebug mv %s %s", name, dest)
		err := os.Rename(name, dest)
		if err != nil {
			log.Debugf("MoveFileToDebug mv error: %v", err)
		}
	}
}
