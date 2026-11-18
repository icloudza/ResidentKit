// Package dto API 的请求与响应结构，与数据库模型解耦。
package dto

import (
	"encoding/json"
	"time"

	"github.com/icloudza/ResidentKit/server/internal/model"
)

// ---- 认证 ----

type LoginRequest struct {
	Username string `json:"username" binding:"required,max=32"`
	Password string `json:"password" binding:"required,max=128"`
	TOTPCode string `json:"totp_code" binding:"omitempty,len=6,numeric"`
}

type TokenResponse struct {
	AccessToken string    `json:"access_token"`
	ExpiresAt   time.Time `json:"expires_at"`
	Admin       Admin     `json:"admin"`
}

type Admin struct {
	ID          uint64     `json:"id"`
	Username    string     `json:"username"`
	Role        string     `json:"role"`
	TOTPEnabled bool       `json:"totp_enabled"`
	LastLoginAt *time.Time `json:"last_login_at,omitempty"`
	LastLoginIP string     `json:"last_login_ip,omitempty"`
}

func AdminFrom(a *model.Admin) Admin {
	return Admin{ID: a.ID, Username: a.Username, Role: a.Role, TOTPEnabled: a.TOTPEnabled,
		LastLoginAt: a.LastLoginAt, LastLoginIP: a.LastLoginIP}
}

// SetupStatus 首次初始化状态。
type SetupStatus struct {
	NeedsSetup bool `json:"needs_setup"`
}

type SetupRequest struct {
	Code     string `json:"code" binding:"required,max=32"`
	Username string `json:"username" binding:"required,max=32"`
	Password string `json:"password" binding:"required,max=128"`
}

// ChangeUsernameRequest 修改用户名，需当前密码（开启两步验证时还需验证码）。
type ChangeUsernameRequest struct {
	Username string `json:"username" binding:"required,max=32"`
	Password string `json:"password" binding:"required,max=128"`
	TOTPCode string `json:"totp_code" binding:"max=8"`
}

type ChangePasswordRequest struct {
	OldPassword string `json:"old_password" binding:"required,max=128"`
	NewPassword string `json:"new_password" binding:"required,min=8,max=128"`
}

type TOTPSetupResponse struct {
	Secret string `json:"secret"`
	URL    string `json:"url"` // otpauth://，可生成二维码
}

type TOTPCodeRequest struct {
	Code string `json:"code" binding:"required,len=6,numeric"`
}

// ---- 设备 ----

type Device struct {
	ID             uint64              `json:"id"`
	DeviceUID      string              `json:"device_uid"`
	Name           string              `json:"name"`
	Alias          string              `json:"alias"`
	Version        string              `json:"version"`
	Online         bool                `json:"online"`
	ConnectedAt    *time.Time          `json:"connected_at,omitempty"`
	LastIP         string              `json:"last_ip"`
	Network        string              `json:"network"`
	RTTMs          int                 `json:"rtt_ms"`
	RemoteCommands map[string][]string `json:"remote_commands"`
	Info           *DeviceInfo         `json:"info,omitempty"`
	Metrics        json.RawMessage     `json:"metrics,omitempty" swaggertype:"object"`
	MetricsAt      *time.Time          `json:"metrics_at,omitempty"`
	FirstSeenAt    time.Time           `json:"first_seen_at"`
	LastSeenAt     time.Time           `json:"last_seen_at"`
}

// DeviceInfo 设备信息（设备端上报）。
type DeviceInfo struct {
	Model      string `json:"model"`       // 机型标识，如 iPhone12,1
	OS         string `json:"os"`          // 系统版本，如 iOS 16.7.10
	AppVersion string `json:"app_version"` // App 版本
}

func DeviceFrom(d *model.Device) Device {
	cmds := map[string][]string{}
	if len(d.RemoteCommands) > 0 {
		_ = json.Unmarshal(d.RemoteCommands, &cmds)
	}
	var info *DeviceInfo
	if len(d.Info) > 0 {
		var v DeviceInfo
		if json.Unmarshal(d.Info, &v) == nil {
			info = &v
		}
	}
	return Device{Info: info, ID: d.ID, DeviceUID: d.DeviceUID, Name: d.Name, Alias: d.Alias, Version: d.Version,
		Online: d.Online, ConnectedAt: d.ConnectedAt, Metrics: json.RawMessage(d.Metrics), MetricsAt: d.MetricsAt,
		LastIP: d.LastIP, Network: d.Network, RTTMs: d.RTTMs, RemoteCommands: cmds,
		FirstSeenAt: d.FirstSeenAt, LastSeenAt: d.LastSeenAt}
}

type UpdateDeviceRequest struct {
	Alias string `json:"alias" binding:"max=64"`
}

// ---- 设备令牌 ----

type DeviceToken struct {
	ID          uint64     `json:"id"`
	Name        string     `json:"name"`
	TokenPrefix string     `json:"token_prefix"`
	DeviceID    *uint64    `json:"device_id,omitempty"`
	LastUsedAt  *time.Time `json:"last_used_at,omitempty"`
	RevokedAt   *time.Time `json:"revoked_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

func DeviceTokenFrom(t *model.DeviceToken) DeviceToken {
	return DeviceToken{ID: t.ID, Name: t.Name, TokenPrefix: t.TokenPrefix, DeviceID: t.DeviceID,
		LastUsedAt: t.LastUsedAt, RevokedAt: t.RevokedAt, CreatedAt: t.CreatedAt}
}

// IssuedToken 签发结果（明文只在签发时出现一次，仅用于自动接入时交给设备）。
type IssuedToken struct {
	DeviceToken
	Token string `json:"token"`
}

// ---- 事件 ----

type Event struct {
	ID        uint64          `json:"id"`
	DeviceID  uint64          `json:"device_id"`
	MsgID     string          `json:"msg_id"`
	Type      string          `json:"type"`
	Level     string          `json:"level"`
	Payload   json.RawMessage `json:"payload,omitempty" swaggertype:"object"`
	DeviceTS  *time.Time      `json:"device_ts,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
}

func EventFrom(e *model.Event) Event {
	return Event{ID: e.ID, DeviceID: e.DeviceID, MsgID: e.MsgID, Type: e.Type, Level: e.Level,
		Payload: json.RawMessage(e.Payload), DeviceTS: e.DeviceTS, CreatedAt: e.CreatedAt}
}

// ---- 指令 ----

type InvokeRequest struct {
	Module    string         `json:"module" binding:"required,max=64"`
	Command   string         `json:"command" binding:"required,max=64"`
	Arguments map[string]any `json:"arguments"`
	// 设备离线时排队等待的时长（秒），0 表示用默认值，-1 表示离线直接失败。
	TTLSeconds int `json:"ttl_seconds" binding:"min=-1,max=604800"`
	// 同步等待结果的时长（秒），0 表示不等待。
	WaitSeconds int `json:"wait_seconds" binding:"min=0,max=60"`
}

type Invocation struct {
	ID           uint64          `json:"id"`
	DeviceID     uint64          `json:"device_id"`
	Module       string          `json:"module"`
	Command      string          `json:"command"`
	Arguments    json.RawMessage `json:"arguments,omitempty" swaggertype:"object"`
	Status       string          `json:"status"`
	Result       json.RawMessage `json:"result,omitempty" swaggertype:"object"`
	ErrorCode    int             `json:"error_code,omitempty"`
	ErrorMessage string          `json:"error_message,omitempty"`
	OperatorID   *uint64         `json:"operator_id,omitempty"`
	APIKeyID     *uint64         `json:"api_key_id,omitempty"`
	IdemKey      string          `json:"idempotency_key,omitempty"`
	SourceIP     string          `json:"source_ip,omitempty"`
	ExpiresAt    time.Time       `json:"expires_at"`
	SentAt       *time.Time      `json:"sent_at,omitempty"`
	FinishedAt   *time.Time      `json:"finished_at,omitempty"`
	CreatedAt    time.Time       `json:"created_at"`
}

func InvocationFrom(i *model.Invocation) Invocation {
	idem := ""
	if i.IdemKey != nil {
		idem = *i.IdemKey
	}
	return Invocation{ID: i.ID, DeviceID: i.DeviceID, Module: i.Module, Command: i.Command,
		APIKeyID: i.APIKeyID, IdemKey: idem, SourceIP: i.SourceIP,
		Arguments: json.RawMessage(i.Arguments), Status: i.Status, Result: json.RawMessage(i.Result),
		ErrorCode: i.ErrorCode, ErrorMessage: i.ErrorMessage, OperatorID: i.OperatorID,
		ExpiresAt: i.ExpiresAt, SentAt: i.SentAt, FinishedAt: i.FinishedAt, CreatedAt: i.CreatedAt}
}

// ---- 审计 ----

type AuditLog struct {
	ID        uint64          `json:"id"`
	AdminID   *uint64         `json:"admin_id,omitempty"`
	Username  string          `json:"username"`
	Action    string          `json:"action"`
	Target    string          `json:"target"`
	IP        string          `json:"ip"`
	Success   bool            `json:"success"`
	Detail    json.RawMessage `json:"detail,omitempty" swaggertype:"object"`
	CreatedAt time.Time       `json:"created_at"`
}

func AuditLogFrom(l *model.AuditLog) AuditLog {
	return AuditLog{ID: l.ID, AdminID: l.AdminID, Username: l.Username, Action: l.Action,
		Target: l.Target, IP: l.IP, Success: l.Success, Detail: json.RawMessage(l.Detail), CreatedAt: l.CreatedAt}
}

// ---- 总览 ----

type HourCount struct {
	Hour  time.Time `json:"hour"`
	Count int64     `json:"count"`
}

type TypeCount struct {
	Type  string `json:"type"`
	Count int64  `json:"count"`
}

// Summary 总览数据。走势均为近 24 小时、按小时，从最早到当前小时。
type Summary struct {
	ServerVersion string `json:"server_version"`
	UptimeSeconds int64  `json:"uptime_seconds"`
	DevicesOnline int64  `json:"devices_online"`
	DevicesTotal  int64  `json:"devices_total"`
	AvgRTTMs      int64  `json:"avg_rtt_ms"` // 在线设备的平均延迟

	Events24h     int64       `json:"events_24h"`
	EventsPrev24h int64       `json:"events_prev_24h"` // 再往前 24 小时，用于环比
	EventsHourly  []HourCount `json:"events_hourly"`
	EventTypes    []TypeCount `json:"event_types"`
	Alerts24h     int64       `json:"alerts_24h"`    // warning + error
	RecentAlerts  []Event     `json:"recent_alerts"` // 最近 5 条 warning / error

	Invocations24h     int64   `json:"invocations_24h"`
	InvocationsOK24h   int64   `json:"invocations_ok_24h"`
	InvocationsDone24h int64   `json:"invocations_done_24h"` // 已出结果（成功 + 失败 + 结果未知 + 过期）
	InvocationsPending int64   `json:"invocations_pending"`  // 排队中 + 已下发未返回
	InvocationsHourly  []int64 `json:"invocations_hourly"`

	Gateway GatewayStat `json:"gateway"`
}

type GatewayStat struct {
	Connections int       `json:"connections"`
	BannedIPs   int       `json:"banned_ips"`
	DenyHourly  []int64   `json:"deny_hourly"`   // 鉴权失败（进程内统计，重启清零）
	RTTHourlyMs []float64 `json:"rtt_hourly_ms"` // 平均延迟（同上）
}

// ---- 分页 ----

// Page 分页结果。
type Page[T any] struct {
	Items    []T   `json:"items"`
	Total    int64 `json:"total"`
	Page     int   `json:"page"`
	PageSize int   `json:"page_size"`
}

// 以下具名类型供 Swagger 文档使用（swag 不解析泛型实例）。

type EventPage struct {
	Items    []Event `json:"items"`
	Total    int64   `json:"total"`
	Page     int     `json:"page"`
	PageSize int     `json:"page_size"`
}

type InvocationPage struct {
	Items    []Invocation `json:"items"`
	Total    int64        `json:"total"`
	Page     int          `json:"page"`
	PageSize int          `json:"page_size"`
}

type AuditLogPage struct {
	Items    []AuditLog `json:"items"`
	Total    int64      `json:"total"`
	Page     int        `json:"page"`
	PageSize int        `json:"page_size"`
}

// ---- OTA ----

// OTAConfig 控制台显示用：是否已配置发布公钥。
type OTAConfig struct {
	Enabled     bool   `json:"enabled"`
	PublicKey   string `json:"public_key,omitempty"`
	RepoEnabled bool   `json:"repo_enabled"` // 软件源已开启：上传时可选择同步到源
}

type Release struct {
	ID        uint64           `json:"id"`
	Version   string           `json:"version"`
	Size      int64            `json:"size"`
	SHA256    string           `json:"sha256"`
	Notes     string           `json:"notes"`
	Status    string           `json:"status"`
	InRepo    bool             `json:"in_repo"` // 已同步到软件源
	CreatedAt time.Time        `json:"created_at"`
	Counts    map[string]int64 `json:"counts"` // 各更新状态的设备数
}

func ReleaseFrom(r *model.Release) Release {
	return Release{ID: r.ID, Version: r.Version, Size: r.Size, SHA256: r.SHA256, Notes: r.Notes,
		Status: r.Status, InRepo: r.InRepo, CreatedAt: r.CreatedAt, Counts: map[string]int64{}}
}

type Rollout struct {
	ID        uint64    `json:"id"`
	ReleaseID uint64    `json:"release_id"`
	Target    string    `json:"target"`
	DeviceIDs []uint64  `json:"device_ids"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func RolloutFrom(r *model.Rollout) Rollout {
	ids := []uint64{}
	if len(r.DeviceIDs) > 0 {
		_ = json.Unmarshal(r.DeviceIDs, &ids)
	}
	return Rollout{ID: r.ID, ReleaseID: r.ReleaseID, Target: r.Target, DeviceIDs: ids, Status: r.Status,
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt}
}

type DeviceUpdate struct {
	ID           uint64    `json:"id"`
	DeviceID     uint64    `json:"device_id"`
	ReleaseID    uint64    `json:"release_id"`
	Version      string    `json:"version"`
	RolloutID    uint64    `json:"rollout_id"`
	Status       string    `json:"status"`
	Error        string    `json:"error,omitempty"`
	InvocationID *uint64   `json:"invocation_id,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func DeviceUpdateFrom(u *model.DeviceUpdate, version string) DeviceUpdate {
	return DeviceUpdate{ID: u.ID, DeviceID: u.DeviceID, ReleaseID: u.ReleaseID, Version: version,
		RolloutID: u.RolloutID, Status: u.Status, Error: u.Error, InvocationID: u.InvocationID,
		CreatedAt: u.CreatedAt, UpdatedAt: u.UpdatedAt}
}

type ReleaseDetail struct {
	Release  Release        `json:"release"`
	Rollouts []Rollout      `json:"rollouts"`
	Devices  []DeviceUpdate `json:"devices"`
}

type CreateRolloutRequest struct {
	Target    string   `json:"target" binding:"required,oneof=all devices"`
	DeviceIDs []uint64 `json:"device_ids" binding:"max=1000"`
}

// DeviceUpdateInfo 设备详情页的更新信息。
type DeviceUpdateInfo struct {
	Version   string         `json:"version"`
	Supported bool           `json:"supported"` // 设备是否开放了 system.update/install
	Latest    *Release       `json:"latest,omitempty"`
	History   []DeviceUpdate `json:"history"`
}

// ---- 远程配置 ----

// ConfigItem 设备声明的一个可配置项（与 daemon RKModule.h 的 +configSchema 对应）。
type ConfigItem struct {
	Key     string   `json:"key"`
	Type    string   `json:"type"` // bool | int | number | string | enum
	Default any      `json:"default"`
	Label   string   `json:"label"`
	Desc    string   `json:"desc,omitempty"`
	Min     *float64 `json:"min,omitempty"`
	Max     *float64 `json:"max,omitempty"`
	Options []string `json:"options,omitempty"`
	MaxLen  int      `json:"max_len,omitempty"`
}

// ConfigSchema 模块 → 配置项。
type ConfigSchema map[string][]ConfigItem

type DeviceConfig struct {
	Supported      bool                      `json:"supported"`
	Schema         ConfigSchema              `json:"schema"`
	Version        int64                     `json:"version"`         // 期望版本，0 表示从未下发
	AppliedVersion int64                     `json:"applied_version"` // 设备已生效的版本
	Status         string                    `json:"status"`          // synced | pending | rejected
	Error          string                    `json:"error,omitempty"`
	Config         map[string]map[string]any `json:"config"` // 与默认值不同的项
	UpdatedAt      *time.Time                `json:"updated_at,omitempty"`
}

type UpdateConfigRequest struct {
	Config map[string]map[string]any `json:"config"`
}

// ---- 自动接入 ----

// EnrollRequest 设备的接入请求（daemon RKEnrollModule）。
type EnrollRequest struct {
	Device    string `json:"device"`
	Name      string `json:"name"`
	Model     string `json:"model"`
	Version   string `json:"version"`
	PublicKey string `json:"public_key"`
	TS        int64  `json:"ts"`
	Signature string `json:"signature"`
	// Ticket 接入码（扫控制台「添加设备」的二维码得到）：有效时直接批准，无效时按普通申请处理。
	Ticket string `json:"ticket,omitempty"`
}

type EnrollResponse struct {
	Status     string `json:"status"` // pending | approved | rejected
	Code       string `json:"code,omitempty"`
	Token      string `json:"token,omitempty"`
	RetryAfter int    `json:"retry_after,omitempty"` // 秒，仅 rejected
}

type Enrollment struct {
	ID        uint64    `json:"id"`
	DeviceUID string    `json:"device_uid"`
	Name      string    `json:"name"`
	Model     string    `json:"model"`
	Version   string    `json:"version"`
	IP        string    `json:"ip"`
	Code      string    `json:"code"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

func EnrollmentFrom(e *model.Enrollment) Enrollment {
	return Enrollment{ID: e.ID, DeviceUID: e.DeviceUID, Name: e.Name, Model: e.Model, Version: e.Version,
		IP: e.IP, Code: e.Code, Status: e.Status, CreatedAt: e.CreatedAt}
}

// ---- 开放 API ----

type CreateAPIKeyRequest struct {
	Name        string   `json:"name" binding:"required,max=64"`
	Scopes      []string `json:"scopes" binding:"required,min=1,max=3"`
	DeviceIDs   []uint64 `json:"device_ids" binding:"max=1000"` // 为空表示全部设备
	AllowedIPs  []string `json:"allowed_ips" binding:"max=50"`  // IP 或 CIDR，为空表示不限
	ExpiresDays int      `json:"expires_days" binding:"min=0,max=3650"`
	Password    string   `json:"password" binding:"required,max=128"`
	TOTPCode    string   `json:"totp_code" binding:"omitempty,len=6,numeric"`
}

type APIKey struct {
	ID         uint64     `json:"id"`
	Name       string     `json:"name"`
	KeyPrefix  string     `json:"key_prefix"`
	Scopes     []string   `json:"scopes"`
	DeviceIDs  []uint64   `json:"device_ids"`
	AllowedIPs []string   `json:"allowed_ips"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	LastUsedIP string     `json:"last_used_ip,omitempty"`
	RevokedAt  *time.Time `json:"revoked_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

func APIKeyFrom(k *model.APIKey) APIKey {
	out := APIKey{ID: k.ID, Name: k.Name, KeyPrefix: k.KeyPrefix, Scopes: []string{}, DeviceIDs: []uint64{},
		AllowedIPs: []string{}, ExpiresAt: k.ExpiresAt, LastUsedAt: k.LastUsedAt, LastUsedIP: k.LastUsedIP,
		RevokedAt: k.RevokedAt, CreatedAt: k.CreatedAt}
	_ = json.Unmarshal(k.Scopes, &out.Scopes)
	if len(k.DeviceIDs) > 0 {
		_ = json.Unmarshal(k.DeviceIDs, &out.DeviceIDs)
	}
	if len(k.AllowedIPs) > 0 {
		_ = json.Unmarshal(k.AllowedIPs, &out.AllowedIPs)
	}
	return out
}

// IssuedAPIKey 明文密钥只在签发时返回一次。
type IssuedAPIKey struct {
	APIKey
	Key string `json:"key"`
}

// OpenInvokeRequest 开放 API 下发指令。幂等键放在请求头 Idempotency-Key。
type OpenInvokeRequest struct {
	Module    string         `json:"module" binding:"required,max=64"`
	Command   string         `json:"command" binding:"required,max=64"`
	Arguments map[string]any `json:"arguments"`
	// 同步等待结果的秒数（0–30），超时返回当前状态，之后用 GET /invocations/{id} 查询。
	WaitSeconds int `json:"wait_seconds" binding:"min=0,max=30"`
	// 设备离线时排队的秒数：0 用默认值（10 分钟），-1 表示离线直接失败。
	TTLSeconds int `json:"ttl_seconds" binding:"min=-1,max=604800"`
}

// OpenAPIInfo 控制台「开放 API」页显示的接入信息。
type OpenAPIInfo struct {
	BaseURL string `json:"base_url"` // 为空表示未配置 gateway.public_url
	CertPin string `json:"cert_pin"` // 服务器证书 DER 的 SHA-256（十六进制）
	// SPKIPin 公钥（SubjectPublicKeyInfo）SHA-256 的 base64，即 curl --pinnedpubkey sha256//… 的值。
	SPKIPin string `json:"spki_pin"`
}

// UpdateAPIKeyRequest 编辑密钥（需再次验证密码）。ExpiresDays：不填表示不变，0 表示永不过期，N 表示从现在起 N 天。
type UpdateAPIKeyRequest struct {
	Name        string   `json:"name" binding:"required,max=64"`
	Scopes      []string `json:"scopes" binding:"required,min=1,max=3"`
	DeviceIDs   []uint64 `json:"device_ids" binding:"max=1000"`
	AllowedIPs  []string `json:"allowed_ips" binding:"max=50"`
	ExpiresDays *int     `json:"expires_days" binding:"omitempty,min=0,max=3650"`
	Password    string   `json:"password" binding:"required,max=128"`
	TOTPCode    string   `json:"totp_code" binding:"omitempty,len=6,numeric"`
}

// APIHour 开放 API 某小时的调用数。
type APIHour struct {
	Hour  time.Time `json:"hour"`
	Total int64     `json:"total"`
	OK    int64     `json:"ok"`
	Bad   int64     `json:"bad"` // 失败 / 结果未知 / 过期
}

type APIKeyStat struct {
	Calls  int64   `json:"calls"`
	OK     int64   `json:"ok"`
	Hourly []int64 `json:"hourly"` // 近 24 小时每小时调用数
}

type APICommandCount struct {
	Module  string `json:"module"`
	Command string `json:"command"`
	Count   int64  `json:"count"`
}

// APIStats 开放 API 近 24 小时概况。
type APIStats struct {
	Calls      int64                 `json:"calls"`
	CallsPrev  int64                 `json:"calls_prev"` // 再往前 24 小时，用于环比
	Succeeded  int64                 `json:"succeeded"`
	Failed     int64                 `json:"failed"`
	Lost       int64                 `json:"lost"` // 结果未知 + 过期
	Running    int64                 `json:"running"`
	AvgMs      float64               `json:"avg_ms"`
	P95Ms      float64               `json:"p95_ms"`
	ActiveKeys int64                 `json:"active_keys"`
	UsedKeys   int64                 `json:"used_keys"` // 24 小时内有调用的密钥
	Hourly     []APIHour             `json:"hourly"`
	Top        []APICommandCount     `json:"top"`
	PerKey     map[uint64]APIKeyStat `json:"per_key"`
}

// APITryRequest 控制台「在线调试」：由服务端代为调用开放 API（免浏览器跨域与证书问题）。
type APITryRequest struct {
	KeyID   uint64          `json:"key_id"`                // 选择已创建的密钥（优先）
	Key     string          `json:"key" binding:"max=128"` // 或直接填明文
	Method  string          `json:"method" binding:"required,oneof=GET POST"`
	Path    string          `json:"path" binding:"required,max=512"` // /open/v1/… 下的路径与查询
	Body    json.RawMessage `json:"body" swaggertype:"object"`
	IdemKey string          `json:"idempotency_key" binding:"max=64"`
}

type APITryResponse struct {
	Status     int             `json:"status"`
	DurationMs float64         `json:"duration_ms"`
	Body       json.RawMessage `json:"body" swaggertype:"object"`
}

type APICallPage struct {
	Items    []Invocation `json:"items"`
	Total    int64        `json:"total"`
	Page     int          `json:"page"`
	PageSize int          `json:"page_size"`
}

// ---- 远程命令说明（开放 API） ----

// CommandParam 命令参数（与配置项格式相同，另加 required）。
type CommandParam struct {
	ConfigItem
	Required bool `json:"required,omitempty"`
}

type CommandField struct {
	Key  string `json:"key"`
	Type string `json:"type"` // bool | int | number | string | object | array
	Desc string `json:"desc,omitempty"`
}

// CommandSpec 一个远程命令的说明，由设备模块声明（+commandSchema）。Declared 为 false 表示设备没有提供说明。
type CommandSpec struct {
	Module   string         `json:"module"`
	Command  string         `json:"command"`
	Label    string         `json:"label"`
	Icon     string         `json:"icon,omitempty"` // 控制台图标名（设备在命令说明里声明）
	Desc     string         `json:"desc,omitempty"`
	Params   []CommandParam `json:"params"`
	Result   []CommandField `json:"result"`
	Declared bool           `json:"declared"`
	Timeout  int            `json:"timeout,omitempty"` // 设备执行上限（秒），超过仍无结果记为结果未知；0 为默认 120
	Devices  []uint64       `json:"devices,omitempty"` // 目录中：支持该命令的设备（近 30 天在线过的）
	Online   bool           `json:"online"`            // 目录中：当前是否有支持该命令的设备在线
}

// CommandResult 开放 API 同步调用成功的返回。
type CommandResult struct {
	InvocationID uint64          `json:"invocation_id"`
	Result       json.RawMessage `json:"result" swaggertype:"object"`
	DurationMs   int64           `json:"duration_ms"`
}

// CommandPending 异步调用、超时或排队时的返回：之后用 GET /invocations/{id} 查询。
type CommandPending struct {
	InvocationID uint64 `json:"invocation_id"`
	Status       string `json:"status"`
	DeviceError  int    `json:"device_error_code,omitempty"`
}

// DeviceActivity 设备活动概览（设备详情页的图表）。
type DeviceActivity struct {
	Range       string           `json:"range"` // 24h | 7d | 30d
	Invocations int64            `json:"invocations"`
	Failed      int64            `json:"failed"` // 失败、结果未知、过期
	Events      int64            `json:"events"`
	Warnings    int64            `json:"warnings"` // 警告与错误级别
	Buckets     []ActivityBucket `json:"buckets"`
}

type ActivityBucket struct {
	At          string `json:"at"` // 桶起点（UTC）
	Invocations int64  `json:"invocations"`
	Failed      int64  `json:"failed"`
	Events      int64  `json:"events"`
	Warnings    int64  `json:"warnings"`
}

// ---- 软件源 ----

// RepoLink 软件源安装链接。Status：active 可用 · expired 已过期 · exhausted 下载次数用完 · revoked 已吊销。
type RepoLink struct {
	ID           uint64     `json:"id"`
	Name         string     `json:"name"`
	TokenPrefix  string     `json:"token_prefix"`
	Viewable     bool       `json:"viewable"` // 可再次查看地址（旧版本生成的链接没有保存令牌）
	Status       string     `json:"status"`
	ExpiresAt    *time.Time `json:"expires_at,omitempty"`
	MaxDownloads uint32     `json:"max_downloads"` // 0 表示不限
	Downloads    uint32     `json:"downloads"`
	LastUsedAt   *time.Time `json:"last_used_at,omitempty"`
	RevokedAt    *time.Time `json:"revoked_at,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
}

// RepoLinkStatus 链接当前状态。
func RepoLinkStatus(l *model.RepoLink, now time.Time) string {
	switch {
	case l.RevokedAt != nil:
		return "revoked"
	case l.ExpiresAt != nil && !now.Before(*l.ExpiresAt):
		return "expired"
	case l.MaxDownloads > 0 && l.Downloads >= l.MaxDownloads:
		return "exhausted"
	}
	return "active"
}

func RepoLinkFrom(l *model.RepoLink, now time.Time) RepoLink {
	return RepoLink{ID: l.ID, Name: l.Name, TokenPrefix: l.TokenPrefix, Viewable: l.TokenEnc != nil, Status: RepoLinkStatus(l, now),
		ExpiresAt: l.ExpiresAt, MaxDownloads: l.MaxDownloads, Downloads: l.Downloads, LastUsedAt: l.LastUsedAt,
		RevokedAt: l.RevokedAt, CreatedAt: l.CreatedAt}
}

// IssuedRepoLink 完整地址只在生成时返回一次（令牌不落库，只存哈希）。
type IssuedRepoLink struct {
	RepoLink
	Path string `json:"path"` // /repo/<令牌>/
}

// RepoOverview 控制台「安装链接」卡片。
type RepoOverview struct {
	Enabled bool       `json:"enabled"`
	Label   string     `json:"label"`
	BaseURL string     `json:"base_url"`         // gateway.public_url，为空时由控制台用当前地址拼接
	Latest  *Release   `json:"latest,omitempty"` // 源里提供的版本：同步到软件源的最新版本
	Links   []RepoLink `json:"links"`
}

// CreateRepoLinkRequest 生成安装链接（需再次验证密码）。ExpiresHours 0 表示不过期；MaxDownloads 0 表示不限。
type CreateRepoLinkRequest struct {
	Name         string `json:"name" binding:"required,max=64"`
	ExpiresHours int    `json:"expires_hours" binding:"min=0,max=8760"`
	MaxDownloads int    `json:"max_downloads" binding:"min=0,max=10000"`
	Password     string `json:"password" binding:"required,max=128"`
	TOTPCode     string `json:"totp_code" binding:"omitempty,len=6,numeric"`
}

// UpdateRepoLinkRequest 修改安装链接。ExpiresHours 不填表示有效期不变，0 表示不过期，N 表示从现在起 N 小时；
// MaxDownloads 0 表示不限。
type UpdateRepoLinkRequest struct {
	Name         string `json:"name" binding:"required,max=64"`
	ExpiresHours *int   `json:"expires_hours" binding:"omitempty,min=0,max=8760"`
	MaxDownloads int    `json:"max_downloads" binding:"min=0,max=10000"`
}

// RepoLinkPath 再次查看安装链接的地址。
type RepoLinkPath struct {
	Path string `json:"path"`
}

// RepoDownload 经安装链接下载安装包的记录。
type RepoDownload struct {
	ID        uint64    `json:"id"`
	Version   string    `json:"version"`
	IP        string    `json:"ip"`
	Machine   string    `json:"machine"`
	Firmware  string    `json:"firmware"`
	UserAgent string    `json:"user_agent"`
	CreatedAt time.Time `json:"created_at"`
}

func RepoDownloadFrom(d *model.RepoDownload) RepoDownload {
	return RepoDownload{ID: d.ID, Version: d.Version, IP: d.IP, Machine: d.Machine, Firmware: d.Firmware,
		UserAgent: d.UserAgent, CreatedAt: d.CreatedAt}
}

// ---- 接入码 ----

// EnrollTicket 接入码。Status：active 可用 · expired 已过期 · exhausted 次数用完 · revoked 已吊销。
type EnrollTicket struct {
	ID          uint64     `json:"id"`
	Name        string     `json:"name"`
	TokenPrefix string     `json:"token_prefix"`
	Status      string     `json:"status"`
	ExpiresAt   time.Time  `json:"expires_at"`
	MaxUses     uint32     `json:"max_uses"` // 0 表示不限
	Uses        uint32     `json:"uses"`
	LastUsedAt  *time.Time `json:"last_used_at,omitempty"`
	RevokedAt   *time.Time `json:"revoked_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

func EnrollTicketStatus(t *model.EnrollTicket, now time.Time) string {
	switch {
	case t.RevokedAt != nil:
		return "revoked"
	case !now.Before(t.ExpiresAt):
		return "expired"
	case t.MaxUses > 0 && t.Uses >= t.MaxUses:
		return "exhausted"
	}
	return "active"
}

func EnrollTicketFrom(t *model.EnrollTicket, now time.Time) EnrollTicket {
	return EnrollTicket{ID: t.ID, Name: t.Name, TokenPrefix: t.TokenPrefix, Status: EnrollTicketStatus(t, now),
		ExpiresAt: t.ExpiresAt, MaxUses: t.MaxUses, Uses: t.Uses, LastUsedAt: t.LastUsedAt, RevokedAt: t.RevokedAt,
		CreatedAt: t.CreatedAt}
}

// IssuedEnrollTicket 接入码明文只在生成时返回一次。
type IssuedEnrollTicket struct {
	EnrollTicket
	Token string `json:"token"`
}

// EnrollTicketOverview 「添加设备」需要的信息：二维码 = <app_scheme>://enroll?u=<server_url>&t=<接入码>[&p=<pin>]。
type EnrollTicketOverview struct {
	AppScheme string         `json:"app_scheme"`
	ServerURL string         `json:"server_url"` // 设备连接地址；未配置 gateway.public_url 时为空，由控制台按当前地址拼
	Path      string         `json:"path"`       // gateway.path
	Pin       string         `json:"pin"`        // 网关自己做 TLS 时的证书指纹（自签证书需要）；经反代时为空
	Tickets   []EnrollTicket `json:"tickets"`
}

// CreateEnrollTicketRequest 生成接入码。MaxUses 0 表示不限。
type CreateEnrollTicketRequest struct {
	Name           string `json:"name" binding:"max=64"`
	ExpiresMinutes int    `json:"expires_minutes" binding:"required,min=1,max=10080"`
	MaxUses        int    `json:"max_uses" binding:"min=0,max=1000"`
}

// SetReleaseRepoRequest 同步到软件源 / 从源里移除。
type SetReleaseRepoRequest struct {
	InRepo bool `json:"in_repo"`
}
