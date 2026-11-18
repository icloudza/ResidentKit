package repository

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/icloudza/ResidentKit/server/internal/model"
)

// TestExplainAllQueries 对每个仓储方法生成的 SQL 执行 EXPLAIN，要求全部走索引（不允许全表扫描）。
//
// 需要一个已迁移、灌入接近真实数据量的库（小表上优化器会直接选全表扫描，结论不可信）：
//
//	RK_EXPLAIN_DSN='user:pass@tcp(127.0.0.1:3306)/rk?parseTime=true' go test ./internal/repository -run TestExplain -v
func TestExplainAllQueries(t *testing.T) {
	dsn := os.Getenv("RK_EXPLAIN_DSN")
	if dsn == "" {
		t.Skip("RK_EXPLAIN_DSN 未设置")
	}
	cap := &sqlCapture{}
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{Logger: cap, DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()

	ctx := context.Background()
	now := time.Now().UTC()
	day := now.Add(-24 * time.Hour)
	week := now.Add(-7 * 24 * time.Hour)
	s := NewSet(db)
	q := ListQuery{Page: 3, PageSize: 20}
	ids := []uint64{1, 2, 3}

	calls := map[string]func(){
		"devices.Upsert":         func() { _ = s.Devices.UpsertOnHello(ctx, &model.Device{DeviceUID: "dev1"}) },
		"devices.SetConfigVer":   func() { _ = s.Devices.SetConfigVersion(ctx, 1, 2) },
		"devices.SetOffline":     func() { _ = s.Devices.SetOffline(ctx, 1, now) },
		"devices.MarkAllOffline": func() { _ = s.Devices.MarkAllOffline(ctx) },
		"devices.Touch":          func() { _ = s.Devices.Touch(ctx, 1, 10, now) },
		"devices.UpdateMetrics":  func() { _ = s.Devices.UpdateMetrics(ctx, 1, []byte("{}"), now) },
		"devices.UpdateInfo":     func() { _ = s.Devices.UpdateInfo(ctx, 1, []byte("{}")) },
		"devices.Get":            func() { _, _ = s.Devices.Get(ctx, 1) },
		"devices.List":           func() { _, _ = s.Devices.List(ctx) },
		"devices.SetAlias":       func() { _ = s.Devices.SetAlias(ctx, 1, "x") },
		"devices.CountOnline":    func() { _, _, _ = s.Devices.CountOnline(ctx) },
		"devices.Delete":         func() { _ = s.Devices.Delete(ctx, 1) },
		"tokens.RevokeByDevice":  func() { _, _ = s.DeviceTokens.RevokeByDevice(ctx, 1, now) },
		"events.DeleteByDevice":  func() { _, _ = s.Events.DeleteByDevice(ctx, 1) },
		"inv.DeleteByDevice":     func() { _, _ = s.Invocations.DeleteByDevice(ctx, 1) },
		"updates.DeleteByDevice": func() { _ = s.DeviceUpdates.DeleteByDevice(ctx, 1) },
		"configs.Delete":         func() { _ = s.DeviceConfigs.Delete(ctx, 1) },

		"tokens.FindActive": func() { _, _ = s.DeviceTokens.FindActiveByHash(ctx, "h") },
		"tokens.FindByHash": func() { _, _ = s.DeviceTokens.FindByHash(ctx, "h") },
		"tokens.Bind":       func() { _, _ = s.DeviceTokens.Bind(ctx, 1, 1) },
		"tokens.MarkUsed":   func() { _ = s.DeviceTokens.MarkUsed(ctx, 1, now) },
		"tokens.Revoke":     func() { _ = s.DeviceTokens.Revoke(ctx, 1, now) },
		"tokens.Delete":     func() { _, _ = s.DeviceTokens.DeleteRevoked(ctx, 1) },
		"tokens.List":       func() { _, _ = s.DeviceTokens.List(ctx) },

		"events.List":        func() { _, _, _ = s.Events.List(ctx, EventFilter{ListQuery: q}) },
		"events.List.device": func() { _, _, _ = s.Events.List(ctx, EventFilter{ListQuery: q, DeviceID: 1}) },
		"events.List.device+all": func() {
			_, _, _ = s.Events.List(ctx, EventFilter{ListQuery: q, DeviceID: 1, Type: "heartbeat", Levels: []string{"warn", "error"}, Since: day})
		},
		"events.List.type":    func() { _, _, _ = s.Events.List(ctx, EventFilter{ListQuery: q, Type: "heartbeat"}) },
		"events.List.levels":  func() { _, _, _ = s.Events.List(ctx, EventFilter{ListQuery: q, Levels: []string{"warn", "error"}}) },
		"events.List.since":   func() { _, _, _ = s.Events.List(ctx, EventFilter{ListQuery: q, Since: day}) },
		"events.CountSince":   func() { _, _ = s.Events.CountSince(ctx, day) },
		"events.CountBetween": func() { _, _ = s.Events.CountBetween(ctx, week, day) },
		"events.TypeCounts":   func() { _, _ = s.Events.TypeCounts(ctx, day, 8) },
		"events.Hourly":       func() { _, _ = s.Events.HourlyCounts(ctx, day) },
		"events.DeleteBefore": func() { _, _ = s.Events.DeleteBefore(ctx, now.Add(-30*24*time.Hour)) },

		"inv.Get":         func() { _, _ = s.Invocations.Get(ctx, 1) },
		"inv.GetByIdem":   func() { _, _ = s.Invocations.GetByIdem(ctx, 1, "idem3") },
		"inv.GetByUID":    func() { _, _ = s.Invocations.GetByUID(ctx, "u") },
		"inv.Transition":  func() { _, _ = s.Invocations.Transition(ctx, 1, []string{"sent"}, map[string]any{"status": "lost"}) },
		"inv.Pending":     func() { _, _ = s.Invocations.PendingForDevice(ctx, 1, now) },
		"inv.SentOverdue": func() { _, _ = s.Invocations.SentOverdue(ctx, now) },
		"inv.SentForDev":  func() { _, _ = s.Invocations.SentForDevice(ctx, 1) },
		"inv.List":        func() { _, _, _ = s.Invocations.List(ctx, InvocationFilter{ListQuery: q}) },
		"inv.List.device": func() { _, _, _ = s.Invocations.List(ctx, InvocationFilter{ListQuery: q, DeviceID: 1}) },
		"inv.List.dev+all": func() {
			_, _, _ = s.Invocations.List(ctx, InvocationFilter{ListQuery: q, DeviceID: 1, Statuses: []string{"failed", "lost"}, Since: day})
		},
		"inv.List.since": func() { _, _, _ = s.Invocations.List(ctx, InvocationFilter{ListQuery: q, Since: day}) },
		"inv.List.status": func() {
			_, _, _ = s.Invocations.List(ctx, InvocationFilter{ListQuery: q, Statuses: []string{"failed"}})
		},
		"inv.DevBuckets":    func() { _, _ = s.Invocations.DeviceBuckets(ctx, 1, week, "%Y-%m-%d", 480) },
		"events.DevBuckets": func() { _, _ = s.Events.DeviceBuckets(ctx, 1, day, "%Y-%m-%d %H", 480) },
		"inv.Expire":        func() { _, _ = s.Invocations.ExpirePending(ctx, now) },
		"inv.StatusCounts":  func() { _, _ = s.Invocations.StatusCounts(ctx, day) },
		"inv.Hourly":        func() { _, _ = s.Invocations.HourlyCounts(ctx, day) },
		"inv.MarkSentLost":  func() { _, _ = s.Invocations.MarkAllSentLost(ctx, now) },
		"inv.DeleteBefore":  func() { _, _ = s.Invocations.DeleteBefore(ctx, now.Add(-90*24*time.Hour)) },

		"admins.Get":        func() { _, _ = s.Admins.Get(ctx, 1) },
		"admins.ByUsername": func() { _, _ = s.Admins.GetByUsername(ctx, "a") },
		"admins.Update":     func() { _ = s.Admins.Update(ctx, 1, map[string]any{"totp_enabled": false}) },
		"admins.Count":      func() { _, _ = s.Admins.Count(ctx) },

		"refresh.FindActive": func() { _, _ = s.RefreshTokens.FindActive(ctx, "h", now) },
		"refresh.Revoke":     func() { _ = s.RefreshTokens.Revoke(ctx, 1, now) },
		"refresh.RevokeAll":  func() { _ = s.RefreshTokens.RevokeAllForAdmin(ctx, 1, now) },
		"refresh.Delete":     func() { _, _ = s.RefreshTokens.DeleteExpired(ctx, now) },

		"audit.List":   func() { _, _, _ = s.Audit.List(ctx, q) },
		"audit.Delete": func() { _, _ = s.Audit.DeleteBefore(ctx, now.Add(-365*24*time.Hour)) },

		"releases.Get":       func() { _, _ = s.Releases.Get(ctx, 1) },
		"releases.ByVersion": func() { _, _ = s.Releases.GetByVersion(ctx, "1.2.0") },
		"releases.All":       func() { _, _ = s.Releases.All(ctx) },
		"releases.SetStatus": func() { _ = s.Releases.SetStatus(ctx, 1, "withdrawn") },

		"rollouts.Get":          func() { _, _ = s.Rollouts.Get(ctx, 1) },
		"rollouts.ByRelease":    func() { _, _ = s.Rollouts.ListByRelease(ctx, 1) },
		"rollouts.Active":       func() { _, _ = s.Rollouts.Active(ctx) },
		"releases.LatestInRepo": func() { _, _ = s.Releases.LatestInRepo(ctx) },
		"releases.SetInRepo":    func() { _ = s.Releases.SetInRepo(ctx, 1, true) },
		"repolinks.Get":         func() { _, _ = s.RepoLinks.Get(ctx, 1) },
		"repolinks.FindByHash":  func() { _, _ = s.RepoLinks.FindByHash(ctx, "h") },
		"repolinks.List":        func() { _, _ = s.RepoLinks.List(ctx) },
		"repolinks.Revoke":      func() { _ = s.RepoLinks.Revoke(ctx, 1, now) },
		"repolinks.Take":        func() { _, _ = s.RepoLinks.TakeDownload(ctx, 1, now) },
		"repolinks.Downloads":   func() { _, _ = s.RepoLinks.Downloads(ctx, 1, 50) },
		"repolinks.Update":      func() { _ = s.RepoLinks.Update(ctx, 1, map[string]any{"name": "x"}) },
		"repolinks.Delete":      func() { _, _ = s.RepoLinks.DeleteRevoked(ctx, 1) },
		"tickets.Get":           func() { _, _ = s.EnrollTickets.Get(ctx, 1) },
		"tickets.FindByHash":    func() { _, _ = s.EnrollTickets.FindByHash(ctx, "h") },
		"tickets.List":          func() { _, _ = s.EnrollTickets.List(ctx) },
		"tickets.Revoke":        func() { _ = s.EnrollTickets.Revoke(ctx, 1, now) },
		"tickets.TakeUse":       func() { _, _ = s.EnrollTickets.TakeUse(ctx, 1, now) },
		"rollouts.Transition":   func() { _, _ = s.Rollouts.Transition(ctx, 1, []string{"active"}, "paused") },

		"updates.Get":         func() { _, _ = s.DeviceUpdates.Get(ctx, 1) },
		"updates.Find":        func() { _, _ = s.DeviceUpdates.Find(ctx, 1, 1) },
		"updates.ByRelease":   func() { _, _ = s.DeviceUpdates.ListByRelease(ctx, 1) },
		"updates.ByReleases":  func() { _, _ = s.DeviceUpdates.ListByReleases(ctx, ids) },
		"updates.ByDevice":    func() { _, _ = s.DeviceUpdates.ListByDevice(ctx, 1, 20) },
		"updates.Unfinished":  func() { _, _ = s.DeviceUpdates.Unfinished(ctx) },
		"updates.Counts":      func() { _, _ = s.DeviceUpdates.Counts(ctx, []uint64{38, 39, 40}) },
		"updates.Transition":  func() { _, _ = s.DeviceUpdates.Transition(ctx, 1, []string{"queued"}, map[string]any{"status": "x"}) },
		"updates.Delete":      func() { _ = s.DeviceUpdates.Delete(ctx, 1) },
		"configs.Get":         func() { _, _ = s.DeviceConfigs.Get(ctx, 1) },
		"configs.SetError":    func() { _ = s.DeviceConfigs.SetError(ctx, 1, 1, "e") },
		"enroll.Get":          func() { _, _ = s.Enrollments.Get(ctx, 1) },
		"enroll.Latest":       func() { _, _ = s.Enrollments.Latest(ctx, "dev1") },
		"enroll.Pending":      func() { _, _ = s.Enrollments.Pending(ctx) },
		"enroll.CountPending": func() { _, _ = s.Enrollments.CountPending(ctx) },
		"enroll.Transition":   func() { _, _ = s.Enrollments.Transition(ctx, 1, []string{"pending"}, map[string]any{"status": "x"}) },
		"enroll.Expire":       func() { _, _ = s.Enrollments.ExpireBefore(ctx, now) },
		"enroll.SwapToken":    func() { _, _ = s.Enrollments.SwapToken(ctx, 1, 1, 2) },

		"apikeys.Get":         func() { _, _ = s.APIKeys.Get(ctx, 1) },
		"apikeys.Update":      func() { _ = s.APIKeys.Update(ctx, 1, map[string]any{"name": "x"}) },
		"apikeys.FindActive":  func() { _, _ = s.APIKeys.FindActiveByHash(ctx, "h") },
		"apikeys.List":        func() { _, _ = s.APIKeys.List(ctx) },
		"apikeys.CountActive": func() { _, _ = s.APIKeys.CountActive(ctx) },
		"apikeys.Revoke":      func() { _ = s.APIKeys.Revoke(ctx, 1, now) },
		"apikeys.Delete":      func() { _, _ = s.APIKeys.DeleteRevoked(ctx, 1) },
		"apikeys.Touch":       func() { _ = s.APIKeys.Touch(ctx, 1, now, "1.1.1.1") },

		"apistats.List":     func() { _, _, _ = s.APIStats.List(ctx, APICallFilter{ListQuery: q, Since: day}) },
		"apistats.List.key": func() { _, _, _ = s.APIStats.List(ctx, APICallFilter{ListQuery: q, KeyID: 1, Since: day}) },
		"apistats.List.all": func() {
			_, _, _ = s.APIStats.List(ctx, APICallFilter{ListQuery: q, KeyID: 1, DeviceID: 1, Statuses: []string{"failed", "lost"}, Since: week})
		},
		"apistats.List.device": func() { _, _, _ = s.APIStats.List(ctx, APICallFilter{ListQuery: q, DeviceID: 1, Since: week}) },
		"apistats.List.status": func() {
			_, _, _ = s.APIStats.List(ctx, APICallFilter{ListQuery: q, Statuses: []string{"failed"}, Since: week})
		},
		"apistats.Hourly":       func() { _, _ = s.APIStats.Hourly(ctx, day) },
		"apistats.StatusCounts": func() { _, _ = s.APIStats.StatusCounts(ctx, day) },
		"apistats.Count":        func() { _, _ = s.APIStats.Count(ctx, week, day) },
		"apistats.TopCommands":  func() { _, _ = s.APIStats.TopCommands(ctx, day, 10) },
		"apistats.Latency":      func() { _, _, _ = s.APIStats.Latency(ctx, day) },
		"apistats.LatencyP95":   func() { _, _ = apiStatsLatencyP95(&apiStatsRepo{db}, ctx, day, 100) },
	}

	bad := 0
	for name, fn := range calls {
		cap.reset()
		fn()
		for _, stmt := range cap.take() {
			verb := strings.ToUpper(strings.Fields(stmt)[0])
			if verb != "SELECT" && verb != "UPDATE" && verb != "DELETE" {
				continue
			}
			for _, r := range explain(t, raw, stmt) {
				line := fmt.Sprintf("%-24s %-14s type=%-6s key=%-28s rows=%-7s %s", name, r.table, r.typ, r.key, r.rows, r.extra)
				if r.table != "" && (r.typ == "ALL" || (r.key == "" && r.typ != "" && r.typ != "const" && r.typ != "system") ||
					fullIndexScan(stmt, r)) {
					bad++
					t.Errorf("未走索引: %s\n    %s", line, stmt)
				} else {
					t.Log(line)
				}
			}
		}
	}
	if bad == 0 {
		t.Logf("全部 %d 个调用的查询均走索引", len(calls))
	}
}

// fullIndexScan 不带 LIMIT 地扫完整个索引（type=index）等同于全表扫描。
// 例外：不带条件的 COUNT(*)（总数只能数整个最小的索引）。
func fullIndexScan(stmt string, r explainRow) bool {
	if r.typ != "index" {
		return false
	}
	up := strings.ToUpper(stmt)
	if strings.Contains(up, " LIMIT ") {
		return false
	}
	return !(strings.HasPrefix(up, "SELECT COUNT(*)") && !strings.Contains(up, " WHERE "))
}

type explainRow struct{ table, typ, key, rows, extra string }

func explain(t *testing.T, db *sql.DB, stmt string) []explainRow {
	rows, err := db.Query("EXPLAIN " + stmt)
	if err != nil {
		t.Errorf("EXPLAIN 失败: %v\n    %s", err, stmt)
		return nil
	}
	defer rows.Close()
	cols, _ := rows.Columns()
	var out []explainRow
	for rows.Next() {
		vals := make([]sql.NullString, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		_ = rows.Scan(ptrs...)
		m := map[string]string{}
		for i, c := range cols {
			m[c] = vals[i].String
		}
		out = append(out, explainRow{m["table"], m["type"], m["key"], m["rows"], m["Extra"]})
	}
	return out
}

// sqlCapture 收集 DryRun 生成的完整 SQL（参数已内联）。
type sqlCapture struct {
	mu    sync.Mutex
	stmts []string
}

func (c *sqlCapture) LogMode(logger.LogLevel) logger.Interface      { return c }
func (c *sqlCapture) Info(context.Context, string, ...interface{})  {}
func (c *sqlCapture) Warn(context.Context, string, ...interface{})  {}
func (c *sqlCapture) Error(context.Context, string, ...interface{}) {}
func (c *sqlCapture) Trace(_ context.Context, _ time.Time, fc func() (string, int64), _ error) {
	s, _ := fc()
	c.mu.Lock()
	c.stmts = append(c.stmts, s)
	c.mu.Unlock()
}
func (c *sqlCapture) reset() { c.mu.Lock(); c.stmts = nil; c.mu.Unlock() }
func (c *sqlCapture) take() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.stmts
}
