package main

type UploadData struct {
	FileName string
	FileSize int64
	Events   chan UploadEvent
}

type UploadEvent struct {
	EventSeq  int64
	EventText string
}
