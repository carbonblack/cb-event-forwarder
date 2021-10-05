package config

import (
	"fmt"
	"github.com/klauspost/compress/gzip"
	"github.com/pierrec/lz4"
	"io"
	"strings"
)

type CompressionType string

const (
	LZ4COMPRESSION  CompressionType = "LZ4"
	GZIPCOMPRESSION CompressionType = "GZIP"
	NOCOMPRESSION   CompressionType = "NONE"
)

func CompressionTypeFromString(typeString string) (CompressionType, error) {
	switch strings.ToUpper(typeString) {
	case string(LZ4COMPRESSION):
		return LZ4COMPRESSION, nil
	case string(GZIPCOMPRESSION):
		return GZIPCOMPRESSION, nil
	case string(NOCOMPRESSION):
		return NOCOMPRESSION, nil
	default:
		return NOCOMPRESSION, fmt.Errorf("compression type %s not recognized (GZIP or LZ4)", typeString)
	}
}

func (config Configuration) FileExtensionForCompressionType() string {
	switch config.CompressionType {
	case LZ4COMPRESSION:
		return ".lz4"
	case GZIPCOMPRESSION:
		return ".gz"
	default:
		switch config.OutputFormat {
		case LEEFOutputFormat:
			return ".leef"
		case JSONOutputFormat:
			return ".json"
		default:
			return ".json"
		}
	}
}

func (config Configuration) WrapWriterWithCompressionSettings(writer io.WriteCloser) (FlushableWriteCloser, error) {
	switch config.CompressionType {
	case LZ4COMPRESSION:
		return lz4.NewWriter(writer), nil
	case GZIPCOMPRESSION:
		return gzip.NewWriterLevel(writer, config.CompressionLevel)
	default:
		return NOPFlushWrappedWriter{WriteCloser: writer}, nil
	}
}

type NOPFlushWrappedWriter struct {
	io.WriteCloser
}

func (writer NOPFlushWrappedWriter) Flush() error {
	return nil
}

type FlushableWriteCloser interface {
	Close() error
	Flush() error
	Write(buf []byte) (int, error)
}
