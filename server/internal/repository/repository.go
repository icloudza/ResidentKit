// Package repository 数据访问层，唯一直接操作数据库的一层；service 依赖接口，便于替换测试。
package repository

import (
	"context"
	"errors"
	"time"

	"github.com/go-sql-driver/mysql"
	"gorm.io/gorm"

	"github.com/icloudza/ResidentKit/server/internal/model"
)

// ErrNotFound 单行查询无结果。
var ErrNotFound = errors.New("record not found")

// IsDuplicate 是否违反唯一索引（MySQL 1062）。
func IsDuplicate(err error) bool {
	var me *mysql.MySQLError
	return errors.As(err, &me) && me.Number == 1062
}

func notFound(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrNotFound
	}
	return err
}

// ListQuery 页码分页，按新到旧。Page 从 1 开始，PageSize 限制在 1–200（默认 20）。
type ListQuery struct {
	Page     int
	PageSize int
}

// Size 规范化后的每页条数。
func (q ListQuery) Size() int {
	if q.PageSize <= 0 {
		return 20
	}
	return min(q.PageSize, 200)
}

// Num 规范化后的页码。
func (q ListQuery) Num() int { return max(q.Page, 1) }

func (q ListQuery) offset() int { return (q.Num() - 1) * q.Size() }

type DeviceRepository interface {
	// UpsertOnHello 新建设备或刷新其上报信息。
	UpsertOnHello(ctx context.Context, d *model.Device) error
	SetOffline(ctx context.Context, id uint64, at time.Time) error
	SetConfigVersion(ctx context.Context, id uint64, v int64) error
	MarkAllOffline(ctx context.Context) error
	Touch(ctx context.Context, id uint64, rttMs int, at time.Time) error
	UpdateMetrics(ctx context.Context, id uint64, metrics []byte, at time.Time) error
	UpdateInfo(ctx context.Context, id uint64, info []byte) error
	Get(ctx context.Context, id uint64) (*model.Device, error)
	List(ctx context.Context) ([]model.Device, error)
	SetAlias(ctx context.Context, id uint64, alias string) error
	CountOnline(ctx context.Context) (online, total int64, err error)
	// Delete 删除设备记录（关联数据由调用方先清理）。
	Delete(ctx context.Context, id uint64) error
}

type DeviceTokenRepository interface {
	Create(ctx context.Context, t *model.DeviceToken) error
	FindActiveByHash(ctx context.Context, hash string) (*model.DeviceToken, error)
	// FindByHash 含已吊销的令牌。
	FindByHash(ctx context.Context, hash string) (*model.DeviceToken, error)
	// Bind 将未绑定的令牌绑定到设备；已绑定其他设备时返回 false。
	Bind(ctx context.Context, tokenID, deviceID uint64) (bool, error)
	MarkUsed(ctx context.Context, id uint64, at time.Time) error
	Revoke(ctx context.Context, id uint64, at time.Time) error
	// DeleteRevoked 只删除已吊销的令牌，未吊销或不存在时返回 false。
	DeleteRevoked(ctx context.Context, id uint64) (bool, error)
	List(ctx context.Context) ([]model.DeviceToken, error)
	// RevokeByDevice 吊销绑定到该设备的全部有效令牌。
	RevokeByDevice(ctx context.Context, deviceID uint64, at time.Time) (int64, error)
}

type EventFilter struct {
	ListQuery
	DeviceID uint64
	Type     string
	Levels   []string  // 为空表示不限
	Since    time.Time // 零值表示不限
}

// TypeCount 某类事件的数量。
type TypeCount struct {
	Type  string `json:"type"`
	Count int64  `json:"count"`
}

type EventRepository interface {
	// InsertIgnore 写入事件，(设备, msg_id) 已存在时忽略并返回 false。
	InsertIgnore(ctx context.Context, e *model.Event) (bool, error)
	List(ctx context.Context, f EventFilter) ([]model.Event, int64, error)
	CountSince(ctx context.Context, since time.Time) (int64, error)
	CountBetween(ctx context.Context, from, to time.Time) (int64, error)
	TypeCounts(ctx context.Context, since time.Time, limit int) ([]TypeCount, error)
	HourlyCounts(ctx context.Context, since time.Time) (map[string]int64, error)
	DeleteBefore(ctx context.Context, t time.Time) (int64, error)
	// DeviceBuckets 某设备 since 之后的事件按时间分桶计数。
	DeviceBuckets(ctx context.Context, deviceID uint64, since time.Time, bucket string, tzOffset int) ([]ActivityBucket, error)
	// DeleteByDevice 分批删除设备的事件，每次最多 10000 条，返回本次删除数。
	DeleteByDevice(ctx context.Context, deviceID uint64) (int64, error)
}

type InvocationRepository interface {
	Create(ctx context.Context, inv *model.Invocation) error
	Get(ctx context.Context, id uint64) (*model.Invocation, error)
	GetByUID(ctx context.Context, uid string) (*model.Invocation, error)
	GetByIdem(ctx context.Context, apiKeyID uint64, key string) (*model.Invocation, error)
	// Transition 仅当状态仍在 fromStatus 中时才更新（防并发覆盖）。
	Transition(ctx context.Context, id uint64, fromStatus []string, updates map[string]any) (bool, error)
	PendingForDevice(ctx context.Context, deviceID uint64, now time.Time) ([]model.Invocation, error)
	SentForDevice(ctx context.Context, deviceID uint64) ([]model.Invocation, error)
	// SentOverdue 已下发、过了结果期限（下发时写入 expires_at）仍无结果的指令。
	SentOverdue(ctx context.Context, now time.Time) ([]model.Invocation, error)
	List(ctx context.Context, f InvocationFilter) ([]model.Invocation, int64, error)
	// DeviceBuckets 某设备 since 之后的指令按时间分桶计数：created_at 平移 tzOffset 分钟后按 bucket（DATE_FORMAT 格式）分组。
	DeviceBuckets(ctx context.Context, deviceID uint64, since time.Time, bucket string, tzOffset int) ([]ActivityBucket, error)
	ExpirePending(ctx context.Context, now time.Time) (int64, error)
	// StatusCounts 统计 since 之后创建的指令各状态数量。
	StatusCounts(ctx context.Context, since time.Time) (map[string]int64, error)
	HourlyCounts(ctx context.Context, since time.Time) (map[string]int64, error)
	MarkAllSentLost(ctx context.Context, now time.Time) (int64, error)
	DeleteBefore(ctx context.Context, t time.Time) (int64, error)
	// DeleteByDevice 分批删除设备的指令，每次最多 10000 条。
	DeleteByDevice(ctx context.Context, deviceID uint64) (int64, error)
}

// InvocationFilter 指令列表筛选。
type InvocationFilter struct {
	ListQuery
	DeviceID uint64   // 0 表示全部设备
	Statuses []string // 为空表示不限
	Since    time.Time
}

// ActivityBucket 一个时间桶内的数量：Total 全部，Bad 失败类（指令：failed/lost/expired；事件：warning/error）。
type ActivityBucket struct {
	Bucket string
	Total  int64
	Bad    int64
}

type AdminRepository interface {
	Create(ctx context.Context, a *model.Admin) error
	Get(ctx context.Context, id uint64) (*model.Admin, error)
	GetByUsername(ctx context.Context, username string) (*model.Admin, error)
	Update(ctx context.Context, id uint64, updates map[string]any) error
	Count(ctx context.Context) (int64, error)
}

type RefreshTokenRepository interface {
	Create(ctx context.Context, t *model.RefreshToken) error
	FindActive(ctx context.Context, hash string, now time.Time) (*model.RefreshToken, error)
	Revoke(ctx context.Context, id uint64, at time.Time) error
	RevokeAllForAdmin(ctx context.Context, adminID uint64, at time.Time) error
	DeleteExpired(ctx context.Context, now time.Time) (int64, error)
}

type AuditRepository interface {
	Insert(ctx context.Context, l *model.AuditLog) error
	List(ctx context.Context, q ListQuery) ([]model.AuditLog, int64, error)
	DeleteBefore(ctx context.Context, t time.Time) (int64, error)
}

// Set 汇总全部 repository，便于装配。
type Set struct {
	Devices       DeviceRepository
	DeviceTokens  DeviceTokenRepository
	Events        EventRepository
	Invocations   InvocationRepository
	Admins        AdminRepository
	RefreshTokens RefreshTokenRepository
	Audit         AuditRepository
	Releases      ReleaseRepository
	Rollouts      RolloutRepository
	RepoLinks     RepoLinkRepository
	EnrollTickets EnrollTicketRepository
	DeviceUpdates DeviceUpdateRepository
	DeviceConfigs DeviceConfigRepository
	Enrollments   EnrollmentRepository
	APIKeys       APIKeyRepository
	APIStats      APIStatsRepository
}

type DeviceConfigRepository interface {
	Get(ctx context.Context, deviceID uint64) (*model.DeviceConfig, error)
	Save(ctx context.Context, c *model.DeviceConfig) error
	SetError(ctx context.Context, deviceID uint64, version int64, msg string) error
	Delete(ctx context.Context, deviceID uint64) error
}

func NewSet(db *gorm.DB) *Set {
	return &Set{
		Devices:       &deviceRepo{db},
		DeviceTokens:  &deviceTokenRepo{db},
		Events:        &eventRepo{db},
		Invocations:   &invocationRepo{db},
		Admins:        &adminRepo{db},
		RefreshTokens: &refreshTokenRepo{db},
		Audit:         &auditRepo{db},
		Releases:      &releaseRepo{db},
		Rollouts:      &rolloutRepo{db},
		RepoLinks:     &repoLinkRepo{db},
		EnrollTickets: &enrollTicketRepo{db},
		DeviceUpdates: &deviceUpdateRepo{db},
		DeviceConfigs: &deviceConfigRepo{db},
		Enrollments:   &enrollmentRepo{db},
		APIKeys:       &apiKeyRepo{db},
		APIStats:      &apiStatsRepo{db},
	}
}
