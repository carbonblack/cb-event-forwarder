package protobufmessageprocessor

type UnixTimeStamp struct {
	EventTimestamp interface{} `json:"timestamp"`
}

type ParentCreateTime struct {
	ParentCreateTimestamp interface{} `json:"parent_create_time"`
}

type Event interface {
	getAsJson() ([]byte, error)
	getAsMap() (map[string]interface{}, error)
}

type BaseEvent struct {
	UnixTimeStamp `json:",inline"`
	CbServer      string `json:"cb_server,omitempty,intern"`
	Type          string `json:"type,intern"`
	SensorId      int32  `json:"sensor_id"`
	ComputerName  string `json:"computer_name"`
	EventType     string `json:"event_type,intern"`
}

type EventMessage struct {
	ForkPid     int32  `json:"fork_pid,omitempty"`
	Pid         int32  `json:"pid,omitempty"`
	ProcessGuid string `json:"process_guid,omitempty"`
	LinkProcess string `json:"link_process,omitempty"`
	LinkSensor  string `json:"link_sensor,omitempty"`
	ProcessPath string `json:"process_path,omitempty"`
}

type HeaderHashes struct {
	Sha256 string `json:"sha256,omitempty"`
	Md5    string `json:"md5,omitempty"`
}

type EventMessageWithHashes struct {
	*EventMessage
	*HeaderHashes
}

type ModloadMessage struct {
	*BaseEvent
	*EventMessage
	Path        string `json:"path"`
	Md5         string `json:"md5"`
	Sha256      string `json:"sha256"`
	CommandLine string `json:"command_line"`
}

type ProcessEvent struct {
	*BaseEvent
	*EventMessage
	Path               string `json:"path"`
	Md5                string `json:"md5"`
	Sha256             string `json:"sha256"`
	CommandLine        string `json:"command_line"`
	ParentPath         string `json:"parent_path"`
	ParentPid          int32  `json:"parent_pid"`
	ParentGuid         int64  `json:"parent_guid"`
	ParentCreateTime   `json:",inline"`
	FilteringKnownDLLS bool   `json:"filtering_known_dlls"`
	ParentMd5          string `json:"parent_md5,omitempty"`
	ParentSha256       string `json:"parent_sha256,omitempty"`
	ExpectFollowonWMd5 bool   `json:"expect_followon_w_md5"`
	LinkParent         string `json:"link_parent,omitempty"`
	Username           string `json:"username,omitempty"`
	Uid                string `json:"uid,omitempty"`
}

type FilemodEvent struct {
	*BaseEvent
	*EventMessageWithHashes
	Path         string `json:"path"`
	TamperSent   bool   `json:"tamper_sent"`
	Tamper       bool   `json:"tamper"`
	FileSha256   string `json:"file_sha256,omitempty"`
	FileMd5      string `json:"file_md5,omitempty"`
	FileTypeName string `json:"filetype_name,omitempty"`
	ActionType   int32  `json:"action_type"`
	Action       string `json:"action,intern"`
	FileType     int32  `json:"file_type"`
}

type NetworkV2Event struct {
	*BaseEvent
	*EventMessageWithHashes
	Protocol    int32
	Domain      string `json:"domain"`
	Direction   string `json:"direction"`
	Ja3         string `json:"ja3,omitempty"`
	Ja3s        string `json:"ja3s,omitempty"`
	LocalIP     string `json:"local_ip"`
	LocalPort   uint16 `json:"local_port"`
	RemoteIP    string `json:"remote_ip"`
	RemotePort  uint16 `json:"remote_port"`
	Proxy       bool   `json:"proxy"`
	ProxyIP     string `json:"proxy_ip,                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                              omitempty"`
	ProxyPort   uint16 `json:"proxy_port,omitempty"`
	ProxyDomain string `json:"proxy_domain, omit_empty"`
}

type NetconEvent struct {
	*BaseEvent
	*EventMessageWithHashes
	RemotePort uint16 `json:"remote_port,omitempty"`
	LocalIp    string `json:"local_ip,omitempty"`
	LocalPort  uint16 `json:"local_port,omitempty"`
	Direction  string `json:"direction"`
	Domain     string `json:"domain"`
	Protocol   int32  `json:"protocol"`
	Port       uint16 `json:"port"`
	Ipv4       string `json:"ipv4"`
	RemoteIp   string `json:"remote_ip,omitempty"`
}

type RegmodEvent struct {
	*BaseEvent
	*EventMessageWithHashes
	Path       string `json:"path"`
	Action     string `json:"action,intern"`
	ActionType int32  `json:"action_type"`
	TamperSent bool   `json:"tamper"`
	Tamper     bool   `json:"tamper_sent"`
}

type ChildprocEvent struct {
	*BaseEvent
	*EventMessage
	ParentGuid       string `json:"parent_guid,omitempty"`
	LinkChild        string `json:"link_child,omitempty"`
	Created          bool   `json:"created"`
	TamperSent       bool   `json:"tamper_sent"`
	Tamper           bool   `json:"tamper"`
	Path             string `json:"path"`
	Md5              string `json:"md5"`
	Sha256           string `json:"sha256"`
	ChildprocType    string `json:"childproc_type,intern"`
	ChildSuppressed  bool   `json:"childproc_suppressed"`
	ChildCommandLine string `json:"childproc_commandline,omitempty"`
	ChildUsername    string `json:"childproc_username,omitempty"`
	ChildPid         int64  `json:"child_pid"`
}

type CrossprocEvent struct {
	*BaseEvent
	*EventMessageWithHashes
	IsTarget          bool   `json:"is_target"`
	CrossProcessType  string `json:"cross_process_type,intern"`
	TargetMd5         string `json:"target_md5"`
	RequestedAccess   uint32 `json:"requested_access,omitempty"`
	LinkTarget        string `json:"link_target,omitempty"`
	TargetPath        string `json:"target_path"`
	TargetSha256      string `json:"target_sha256"`
	TargetPid         uint32 `json:"target_pid"`
	TargetCreateTime  uint64 `json:"target_create_time"`
	TargetProcessGuid string `json:"target_process_guid"`
}

type EmetEvent struct {
	*BaseEvent
	*EventMessageWithHashes
	LogMessage    string `json:"log_message"`
	Mitigation    string `json:"mitigation,intern"`
	Blocked       bool   `json:"blocked"`
	EmetTimestamp uint64 `json:"emet_timestamp"`
	LogId         uint64 `json:"log_id"`
}

type ScriptExEvent struct {
	*BaseEvent
	*EventMessageWithHashes
	ScriptSha256 string `json:"script_sha256"`
	Script       string `json:"script"`
}

type TamperAlert struct {
	*BaseEvent
	TamperType string `json:"tamper_type"`
}

type BlockedEvent struct {
	*BaseEvent
	Md5               string `json:"md5"`
	Path              string `json:"path"`
	LinkTarget        string `json:"link_target,omitempty"`
	ProcessGuid       string `json:"process_guid,omitempty"`
	ProcessCreateTime uint64 `json:"process_create_time,omitempty"`
	CommandLine       string `json:"command_line,omitempty"`
	Pid               int32  `json:"pid,omitempty"`
	Uid               string `json:"uid,omitempty"`
	Username          string `json:"username,omitempty"`
	BlockedError      uint32 `json:"blocked_error,omitempty"`
	BlockedEvent      string `json:"blocked_event,omitempty"`
	BlockedReason     string `json:"blocked_reason,omitempty"`
	BlockedResult     string `json:"blocked_result,omitempty"`
}

type ModInfoEvent struct {
	*BaseEvent
	Digsig                 *DigSigResult `json:"digsig"`
	Utf8CopiedModuleLength uint32        `json:"utf_8_copied_module_length"`
	Utf8FileDescription    string        `json:"utf_8_file_dscription"`
	Utf8CompanyName        string        `json:"utf_8_company_name"`
	Utf8Comments           string        `json:"utf_8_comments"`
	Utf8FileVersion        string        `json:"utf_8_file_version"`
	Utf8LegalCopyRight     string        `json:"utf_8_legal_copyright"`
	Utf8LegalTradeMark     string        `json:"utf_8_legal_trademark"`
	Utf8InternalName       string        `json:"utf_8_internal_name"`
	Utf8ProductName        string        `json:"utf_8_product_name"`
	Utf8OriginalFileName   string        `json:"utf_8_original_file_name"`
	Utf8ProductDescription string        `json:"utf_8_product_description"`
	Utf8ProductVersion     string        `json:"utf_8_product_version"`
	Utf8SpecialBuild       string        `json:"utf_8_special_build"`
	ImageFileHeader        []byte        `json:"image_file_header"`
	Utf8OnDiskFileName     string        `json:"utf_8_on_disk_filename"`
	Icon                   []byte        `json:"icon"`
	Utf8PrivateBuild       string        `json:"utf_8_private_build"`
	Size                   uint64        `json:"size"`
	Sha256                 string        `json:"sha256"`
	Md5                    string        `json:"md5"`
}

type DigSigResult struct {
	Result      string `json:"result"`
	Publisher   string `json:"publisher"`
	ProgramName string `json:"program_name"`
	IssuerName  string `json:"issuer_name"`
	ResultCode  string `json:"result_code"`
	SubjectName string `json:"subject_name"`
	SignTime    string `json:"sign_time"`
}
