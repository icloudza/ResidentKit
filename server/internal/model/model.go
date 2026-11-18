// Package model GORM 模型，与 migrations/*.sql 一一对应；表结构以迁移为准，不用 AutoMigrate。
package model

import (
	"time"

	"gorm.io/datatypes"
)

type Device struct {
	ID             uint64 `gorm:"primaryKey"`
	DeviceUID      string `gorm:"column:device_uid"`
	Name           string
	Alias          string
	Version        string
	Online         bool
	ConnectedAt    *time.Time
	LastIP         string `gorm:"column:last_ip"`
	Network        string
	RTTMs          int            `gorm:"column:rtt_ms"`
	RemoteCommands datatypes.JSON `gorm:"column:remote_commands"`
	ConfigSchema   datatypes.JSON `gorm:"column:config_schema"`
	CommandSchema  datatypes.JSON `gorm:"column:command_schema"`
	Info           datatypes.JSON
	ConfigVersion  int64
	Metrics        datatypes.JSON
	MetricsAt      *time.Time
	FirstSeenAt    time.Time
	LastSeenAt     time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type DeviceToken struct {
	ID          uint64 `gorm:"primaryKey"`
	Name        string
	TokenHash   string
	TokenPrefix string
	DeviceID    *uint64
	CreatedBy   *uint64
	LastUsedAt  *time.Time
	RevokedAt   *time.Time
	CreatedAt   time.Time
}

type Event struct {
	ID        uint64 `gorm:"primaryKey"`
	DeviceID  uint64
	MsgID     string `gorm:"column:msg_id"`
	Type      string
	Level     string
	Payload   datatypes.JSON
	DeviceTS  *time.Time `gorm:"column:device_ts"`
	CreatedAt time.Time
}

// 事件级别（docs/events.md）。
const (
	LevelInfo    = "info"
	LevelNotice  = "notice"
	LevelWarning = "warning"
	LevelError   = "error"
)

// ValidLevel 规范化级别，未知值按 info 处理。
func ValidLevel(s string) string {
	switch s {
	case LevelNotice, LevelWarning, LevelError:
		return s
	}
	return LevelInfo
}

// 指令状态。
const (
	InvPending   = "pending"   // 设备离线，待其上线后下发
	InvSent      = "sent"      // 已下发，等待结果
	InvSucceeded = "succeeded" // 设备返回成功
	InvFailed    = "failed"    // 设备返回错误
	InvExpired   = "expired"   // 过期前未能下发
	InvLost      = "lost"      // 已下发但结果返回前连接断开
)

type Invocation struct {
	ID           uint64 `gorm:"primaryKey"`
	UID          string `gorm:"column:uid"`
	DeviceID     uint64
	Module       string
	Command      string
	Arguments    datatypes.JSON
	Status       string
	Result       datatypes.JSON
	ErrorCode    int
	ErrorMessage string
	OperatorID   *uint64
	APIKeyID     *uint64 `gorm:"column:api_key_id"`
	IdemKey      *string `gorm:"column:idempotency_key"`
	SourceIP     string  `gorm:"column:source_ip"`
	ExpiresAt    time.Time
	SentAt       *time.Time
	FinishedAt   *time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// Finished 是否已到终态。
func (i *Invocation) Finished() bool {
	return i.Status != InvPending && i.Status != InvSent
}

type Admin struct {
	ID           uint64 `gorm:"primaryKey"`
	Username     string
	PasswordHash string
	Role         string
	TOTPSecret   string `gorm:"column:totp_secret"`
	TOTPEnabled  bool   `gorm:"column:totp_enabled"`
	FailedCount  int
	LockedUntil  *time.Time
	LastLoginAt  *time.Time
	LastLoginIP  string `gorm:"column:last_login_ip"`
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type RefreshToken struct {
	ID        uint64 `gorm:"primaryKey"`
	AdminID   uint64
	TokenHash string
	UserAgent string
	IP        string `gorm:"column:ip"`
	ExpiresAt time.Time
	RevokedAt *time.Time
	CreatedAt time.Time
}

type AuditLog struct {
	ID        uint64 `gorm:"primaryKey"`
	AdminID   *uint64
	Username  string
	Action    string
	Target    string
	IP        string `gorm:"column:ip"`
	Success   bool
	Detail    datatypes.JSON
	CreatedAt time.Time
}

// ---- OTA ----

const (
	ReleasePublished = "published"
	ReleaseWithdrawn = "withdrawn"

	RolloutActive    = "active"
	RolloutPaused    = "paused"
	RolloutCancelled = "cancelled"
	RolloutCompleted = "completed"

	UpdQueued      = "queued"      // 已排入远程指令
	UpdDownloading = "downloading" // 设备已接受，下载 / 校验 / 安装中
	UpdInstalled   = "installed"
	UpdFailed      = "failed"
	UpdRolledBack  = "rolled_back"
	UpdUnsupported = "unsupported" // 设备版本过旧，没有 system.update 模块
)

type Release struct {
	ID        uint64 `gorm:"primaryKey"`
	Version   string
	Package   string
	Size      int64
	SHA256    string `gorm:"column:sha256"`
	Signature string
	Notes     string
	FileName  string
	Status    string
	InRepo    bool // 同步到软件源：源里提供已同步且未撤回的最新版本
	CreatedBy *uint64
	CreatedAt time.Time
	UpdatedAt time.Time
}

type Rollout struct {
	ID        uint64 `gorm:"primaryKey"`
	ReleaseID uint64
	Target    string
	DeviceIDs datatypes.JSON `gorm:"column:device_ids"`
	Status    string
	CreatedBy *uint64
	CreatedAt time.Time
	UpdatedAt time.Time
}

type DeviceUpdate struct {
	ID           uint64 `gorm:"primaryKey"`
	DeviceID     uint64
	ReleaseID    uint64
	RolloutID    uint64
	Status       string
	Error        string
	InvocationID *uint64
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// UpdateFinished 是否已到终态（不再自动处理）。
func (u *DeviceUpdate) Finished() bool {
	return u.Status != UpdQueued && u.Status != UpdDownloading
}

// DeviceConfig 服务端为设备保存的期望配置（远程配置）。
type DeviceConfig struct {
	DeviceID  uint64 `gorm:"primaryKey;autoIncrement:false"`
	Version   int64
	Config    datatypes.JSON
	Error     string
	UpdatedBy *uint64
	UpdatedAt time.Time
}

// ---- 自动接入 ----

const (
	EnrollPending   = "pending"   // 等待管理员处理
	EnrollApproved  = "approved"  // 已同意，等设备来领令牌
	EnrollCompleted = "completed" // 令牌已发给设备
	EnrollRejected  = "rejected"
	EnrollExpired   = "expired" // 超时未处理
)

type Enrollment struct {
	ID        uint64 `gorm:"primaryKey"`
	DeviceUID string `gorm:"column:device_uid"`
	Name      string
	Model     string
	Version   string
	PublicKey string
	IP        string `gorm:"column:ip"`
	Code      string
	Status    string
	TokenID   *uint64
	DecidedBy *uint64
	DecidedAt *time.Time
	TicketID  *uint64 // 经接入码直接批准时的接入码
	CreatedAt time.Time
	UpdatedAt time.Time
}

// EnrollTicket 接入码（控制台「添加设备」二维码）：设备带着它申请接入时直接批准。
type EnrollTicket struct {
	ID          uint64 `gorm:"primaryKey"`
	Name        string
	TokenHash   string
	TokenPrefix string
	ExpiresAt   time.Time
	MaxUses     uint32 // 0 表示不限
	Uses        uint32
	LastUsedAt  *time.Time
	RevokedAt   *time.Time
	CreatedBy   *uint64
	CreatedAt   time.Time
}

// ---- 开放 API ----

// API 密钥的权限。
const (
	ScopeDevicesRead    = "devices:read"
	ScopeCommandsInvoke = "commands:invoke"
	ScopeEventsRead     = "events:read"
)

type APIKey struct {
	ID         uint64 `gorm:"primaryKey"`
	Name       string
	KeyHash    string
	KeyPrefix  string
	Scopes     datatypes.JSON
	DeviceIDs  datatypes.JSON `gorm:"column:device_ids"`
	AllowedIPs datatypes.JSON `gorm:"column:allowed_ips"`
	ExpiresAt  *time.Time
	CreatedBy  *uint64
	LastUsedAt *time.Time
	LastUsedIP string `gorm:"column:last_used_ip"`
	RevokedAt  *time.Time
	CreatedAt  time.Time
}

func (APIKey) TableName() string { return "api_keys" }

// ---- 软件源 ----

// RepoLink 软件源安装链接（/repo/<令牌>/）。
type RepoLink struct {
	ID           uint64 `gorm:"primaryKey"`
	Name         string
	TokenHash    string
	TokenPrefix  string
	TokenEnc     *string // 加密保存的令牌，用于再次查看；旧链接为空
	ExpiresAt    *time.Time
	MaxDownloads uint32 // 0 表示不限
	Downloads    uint32
	LastUsedAt   *time.Time
	RevokedAt    *time.Time
	CreatedBy    *uint64
	CreatedAt    time.Time
}

// RepoDownload 经安装链接下载安装包的记录。
type RepoDownload struct {
	ID        uint64 `gorm:"primaryKey"`
	LinkID    uint64
	Version   string
	IP        string `gorm:"column:ip"`
	Machine   string
	Firmware  string
	UserAgent string
	CreatedAt time.Time
}
