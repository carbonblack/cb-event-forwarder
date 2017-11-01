package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	log "github.com/sirupsen/logrus"
	"gopkg.in/h2non/filetype.v1"
	"net"
	"os"
	"path/filepath"
)

/*
 * conversion routines
 */

func WindowsTimeToUnixTime(windows_time int64) int64 {
	// number of milliseconds between Jan 1st 1601 and Jan 1st 1970
	var time_shift int64
	time_shift = 11644473600000

	if windows_time == 0 {
		return windows_time
	}

	windows_time /= 10000      // ns to ms
	windows_time -= time_shift // since 1601 to since 1970
	windows_time /= 1000
	return windows_time
}

func MakeGUID(sensor_id, pid int32, create_time int64) string {
	guid_part_1 := uint32(sensor_id)
	guid_part_2 := uint16(pid >> 16)
	guid_part_3 := uint16(pid & 0xffff)
	guid_part_4 := uint16(create_time >> 48)
	guid_part_5 := uint16((create_time >> 32) & 0xffff)
	guid_part_6 := uint32(create_time & 0xffffffff)

	return fmt.Sprintf("%08x-%04x-%04x-%04x-%04x%08x", guid_part_1, guid_part_2, guid_part_3, guid_part_4,
		guid_part_5, guid_part_6)
}

func GetIPv4Address(addr uint32) string {
	buf := bytes.Buffer{}

	if err := binary.Write(&buf, binary.LittleEndian, addr); err != nil {
		return "<unknown>"
	}

	b := buf.Bytes()

	return net.IPv4(b[0], b[1], b[2], b[3]).String()
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

func ntohs(p uint16) uint16 {
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
	stats, stats_err := fp.Stat()
	if stats_err == nil {
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
	} else {
		return false
	}
}

func MoveFileToDebug(name string) {
	baseName := filepath.Base(name)
	dest := fmt.Sprintf("%s/%s%s", config.DebugStore, baseName, ".debug")
	log.Debugf("slunk_hec_behavior::moveFileToDebug mv %s %s", name, dest)
	err := os.Rename(name, dest)
	if err != nil {
		log.Debugf("MoveFileToDebug mv error: %v", err)
	}
}
