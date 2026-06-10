package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/channelmonitor"
	"github.com/Wei-Shaw/sub2api/ent/channelmonitorhistory"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

// channelMonitorRepository
//
//   - CRUD
//   -
type channelMonitorRepository struct {
	client *dbent.Client
	db     *sql.DB
}

// NewChannelMonitorRepository
func NewChannelMonitorRepository(client *dbent.Client, db *sql.DB) service.ChannelMonitorRepository {
	return &channelMonitorRepository{client: client, db: db}
}

// ---------- CRUD ----------

func (r *channelMonitorRepository) Create(ctx context.Context, m *service.ChannelMonitor) error {
	client := clientFromContext(ctx, r.client)
	builder := client.ChannelMonitor.Create().
		SetName(m.Name).
		SetProvider(channelmonitor.Provider(m.Provider)).
		SetAPIMode(defaultAPIModeRepo(m.APIMode)).
		SetEndpoint(m.Endpoint).
		SetAPIKeyEncrypted(m.APIKey). // 调用方传入的已是密文
		SetPrimaryModel(m.PrimaryModel).
		SetExtraModels(emptySliceIfNil(m.ExtraModels)).
		SetGroupName(m.GroupName).
		SetEnabled(m.Enabled).
		SetIntervalSeconds(m.IntervalSeconds).
		SetCreatedBy(m.CreatedBy).
		SetExtraHeaders(emptyHeadersIfNilRepo(m.ExtraHeaders)).
		SetBodyOverrideMode(defaultBodyModeRepo(m.BodyOverrideMode))
	if m.TemplateID != nil {
		builder = builder.SetTemplateID(*m.TemplateID)
	}
	if m.BodyOverride != nil {
		builder = builder.SetBodyOverride(m.BodyOverride)
	}

	created, err := builder.Save(ctx)
	if err != nil {
		return translatePersistenceError(err, service.ErrChannelMonitorNotFound, nil)
	}
	m.ID = created.ID
	m.CreatedAt = created.CreatedAt
	m.UpdatedAt = created.UpdatedAt
	return nil
}

func (r *channelMonitorRepository) GetByID(ctx context.Context, id int64) (*service.ChannelMonitor, error) {
	row, err := r.client.ChannelMonitor.Query().
		Where(channelmonitor.IDEQ(id)).
		Only(ctx)
	if err != nil {
		return nil, translatePersistenceError(err, service.ErrChannelMonitorNotFound, nil)
	}
	return entToServiceMonitor(row), nil
}

func (r *channelMonitorRepository) Update(ctx context.Context, m *service.ChannelMonitor) error {
	client := clientFromContext(ctx, r.client)
	updater := client.ChannelMonitor.UpdateOneID(m.ID).
		SetName(m.Name).
		SetProvider(channelmonitor.Provider(m.Provider)).
		SetAPIMode(defaultAPIModeRepo(m.APIMode)).
		SetEndpoint(m.Endpoint).
		SetAPIKeyEncrypted(m.APIKey).
		SetPrimaryModel(m.PrimaryModel).
		SetExtraModels(emptySliceIfNil(m.ExtraModels)).
		SetGroupName(m.GroupName).
		SetEnabled(m.Enabled).
		SetIntervalSeconds(m.IntervalSeconds).
		SetExtraHeaders(emptyHeadersIfNilRepo(m.ExtraHeaders)).
		SetBodyOverrideMode(defaultBodyModeRepo(m.BodyOverrideMode))
	if m.TemplateID != nil {
		updater = updater.SetTemplateID(*m.TemplateID)
	} else {
		updater = updater.ClearTemplateID()
	}
	if m.BodyOverride != nil {
		updater = updater.SetBodyOverride(m.BodyOverride)
	} else {
		updater = updater.ClearBodyOverride()
	}

	updated, err := updater.Save(ctx)
	if err != nil {
		return translatePersistenceError(err, service.ErrChannelMonitorNotFound, nil)
	}
	m.UpdatedAt = updated.UpdatedAt
	return nil
}

func (r *channelMonitorRepository) Delete(ctx context.Context, id int64) error {
	client := clientFromContext(ctx, r.client)
	if err := client.ChannelMonitor.DeleteOneID(id).Exec(ctx); err != nil {
		return translatePersistenceError(err, service.ErrChannelMonitorNotFound, nil)
	}
	return nil
}

func (r *channelMonitorRepository) List(ctx context.Context, params service.ChannelMonitorListParams) ([]*service.ChannelMonitor, int64, error) {
	q := r.client.ChannelMonitor.Query()
	if params.Provider != "" {
		q = q.Where(channelmonitor.ProviderEQ(channelmonitor.Provider(params.Provider)))
	}
	if params.Enabled != nil {
		q = q.Where(channelmonitor.EnabledEQ(*params.Enabled))
	}
	if s := strings.TrimSpace(params.Search); s != "" {
		q = q.Where(channelmonitor.Or(
			channelmonitor.NameContainsFold(s),
			channelmonitor.GroupNameContainsFold(s),
			channelmonitor.PrimaryModelContainsFold(s),
		))
	}

	total, err := q.Count(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("count monitors: %w", err)
	}

	pageSize := params.PageSize
	if pageSize <= 0 {
		pageSize = 20
	}
	page := params.Page
	if page <= 0 {
		page = 1
	}

	rows, err := q.
		Order(dbent.Desc(channelmonitor.FieldID)).
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		All(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("list monitors: %w", err)
	}

	out := make([]*service.ChannelMonitor, 0, len(rows))
	for _, row := range rows {
		out = append(out, entToServiceMonitor(row))
	}
	return out, int64(total), nil
}

// ----------

func (r *channelMonitorRepository) ListEnabled(ctx context.Context) ([]*service.ChannelMonitor, error) {
	rows, err := r.client.ChannelMonitor.Query().
		Where(channelmonitor.EnabledEQ(true)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list enabled monitors: %w", err)
	}
	out := make([]*service.ChannelMonitor, 0, len(rows))
	for _, row := range rows {
		out = append(out, entToServiceMonitor(row))
	}
	return out, nil
}

func (r *channelMonitorRepository) MarkChecked(ctx context.Context, id int64, checkedAt time.Time) error {
	client := clientFromContext(ctx, r.client)
	if err := client.ChannelMonitor.UpdateOneID(id).
		SetLastCheckedAt(checkedAt).
		Exec(ctx); err != nil {
		return translatePersistenceError(err, service.ErrChannelMonitorNotFound, nil)
	}
	return nil
}

func (r *channelMonitorRepository) InsertHistoryBatch(ctx context.Context, rows []*service.ChannelMonitorHistoryRow) error {
	if len(rows) == 0 {
		return nil
	}
	client := clientFromContext(ctx, r.client)
	bulk := make([]*dbent.ChannelMonitorHistoryCreate, 0, len(rows))
	for _, row := range rows {
		c := client.ChannelMonitorHistory.Create().
			SetMonitorID(row.MonitorID).
			SetModel(row.Model).
			SetStatus(channelmonitorhistory.Status(row.Status)).
			SetMessage(row.Message).
			SetCheckedAt(row.CheckedAt)
		if row.LatencyMs != nil {
			c = c.SetLatencyMs(*row.LatencyMs)
		}
		if row.PingLatencyMs != nil {
			c = c.SetPingLatencyMs(*row.PingLatencyMs)
		}
		bulk = append(bulk, c)
	}
	if _, err := client.ChannelMonitorHistory.CreateBulk(bulk...).Save(ctx); err != nil {
		return fmt.Errorf("insert history bulk: %w", err)
	}
	return nil
}

// DeleteHistoryBefore < before
// (checked_at)
func (r *channelMonitorRepository) DeleteHistoryBefore(ctx context.Context, before time.Time) (int64, error) {
	return deleteChannelMonitorBatched(ctx, r.db, channelMonitorPruneHistorySQL, before)
}

// ListHistory
// model
func (r *channelMonitorRepository) ListHistory(ctx context.Context, monitorID int64, model string, limit int) ([]*service.ChannelMonitorHistoryEntry, error) {
	q := r.client.ChannelMonitorHistory.Query().
		Where(channelmonitorhistory.MonitorIDEQ(monitorID))
	if strings.TrimSpace(model) != "" {
		q = q.Where(channelmonitorhistory.ModelEQ(model))
	}
	rows, err := q.
		Order(dbent.Desc(channelmonitorhistory.FieldCheckedAt)).
		Limit(limit).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list history: %w", err)
	}
	out := make([]*service.ChannelMonitorHistoryEntry, 0, len(rows))
	for _, row := range rows {
		entry := &service.ChannelMonitorHistoryEntry{
			ID:            row.ID,
			Model:         row.Model,
			Status:        string(row.Status),
			LatencyMs:     row.LatencyMs,
			PingLatencyMs: row.PingLatencyMs,
			Message:       row.Message,
			CheckedAt:     row.CheckedAt,
		}
		out = append(out, entry)
	}
	return out, nil
}

// ----------

// ListLatestPerModel (monitor_id, model)
// (monitor_id, model, checked_at DESC)
func (r *channelMonitorRepository) ListLatestPerModel(ctx context.Context, monitorID int64) ([]*service.ChannelMonitorLatest, error) {
	const q = `
		SELECT DISTINCT ON (model)
		    model, status, latency_ms, ping_latency_ms, checked_at
		FROM channel_monitor_histories
		WHERE monitor_id = $1
		ORDER BY model, checked_at DESC
	`
	rows, err := r.db.QueryContext(ctx, q, monitorID)
	if err != nil {
		return nil, fmt.Errorf("query latest per model: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]*service.ChannelMonitorLatest, 0)
	for rows.Next() {
		l := &service.ChannelMonitorLatest{}
		var latency, ping sql.NullInt64
		if err := rows.Scan(&l.Model, &l.Status, &latency, &ping, &l.CheckedAt); err != nil {
			return nil, fmt.Errorf("scan latest row: %w", err)
		}
		assignNullInt(&l.LatencyMs, latency)
		assignNullInt(&l.PingLatencyMs, ping)
		out = append(out, l)
	}
	return out, rows.Err()
}

// assignNullInt *int
// { v := int(...) ... }
func assignNullInt(dst **int, n sql.NullInt64) {
	if !n.Valid {
		return
	}
	v := int(n.Int64)
	*dst = &v
}

// ComputeAvailability
// "" = status IN (operational, degraded)。
//
// <= 30
//
func (r *channelMonitorRepository) ComputeAvailability(ctx context.Context, monitorID int64, windowDays int) ([]*service.ChannelMonitorAvailability, error) {
	if windowDays <= 0 {
		windowDays = 7
	}
	const q = `
		SELECT model,
		       COUNT(*)                                                             AS total,
		       COUNT(*) FILTER (WHERE status IN ('operational','degraded'))         AS ok,
		       CASE WHEN COUNT(latency_ms) > 0
		            THEN SUM(latency_ms) FILTER (WHERE latency_ms IS NOT NULL)::float8 / COUNT(latency_ms)
		            ELSE NULL END                                                   AS avg_latency_ms
		FROM channel_monitor_histories
		WHERE monitor_id = $1
		  AND checked_at >= NOW() - ($2::int || ' days')::interval
		GROUP BY model
	`
	rows, err := r.db.QueryContext(ctx, q, monitorID, windowDays)
	if err != nil {
		return nil, fmt.Errorf("query availability: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]*service.ChannelMonitorAvailability, 0)
	for rows.Next() {
		row, err := scanAvailabilityRow(rows, windowDays)
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// scanAvailabilityRow (model, total, ok, avg_latency)
//
func scanAvailabilityRow(rows interface{ Scan(...any) error }, windowDays int) (*service.ChannelMonitorAvailability, error) {
	row := &service.ChannelMonitorAvailability{WindowDays: windowDays}
	var avgLatency sql.NullFloat64
	if err := rows.Scan(&row.Model, &row.TotalChecks, &row.OperationalChecks, &avgLatency); err != nil {
		return nil, fmt.Errorf("scan availability row: %w", err)
	}
	finalizeAvailabilityRow(row, avgLatency)
	return row, nil
}

// finalizeAvailabilityRow
// *int。
func finalizeAvailabilityRow(row *service.ChannelMonitorAvailability, avgLatency sql.NullFloat64) {
	if row.TotalChecks > 0 {
		row.AvailabilityPct = float64(row.OperationalChecks) * 100.0 / float64(row.TotalChecks)
	}
	if avgLatency.Valid {
		v := int(avgLatency.Float64)
		row.AvgLatencyMs = &v
	}
}

// ListLatestForMonitorIDs "(monitor_id, model) "
// (monitor_id, model, checked_at DESC)
func (r *channelMonitorRepository) ListLatestForMonitorIDs(ctx context.Context, ids []int64) (map[int64][]*service.ChannelMonitorLatest, error) {
	out := make(map[int64][]*service.ChannelMonitorLatest, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	const q = `
		SELECT DISTINCT ON (monitor_id, model)
		    monitor_id, model, status, latency_ms, ping_latency_ms, checked_at
		FROM channel_monitor_histories
		WHERE monitor_id = ANY($1)
		ORDER BY monitor_id, model, checked_at DESC
	`
	rows, err := r.db.QueryContext(ctx, q, pq.Array(ids))
	if err != nil {
		return nil, fmt.Errorf("query latest batch: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var monitorID int64
		l := &service.ChannelMonitorLatest{}
		var latency, ping sql.NullInt64
		if err := rows.Scan(&monitorID, &l.Model, &l.Status, &latency, &ping, &l.CheckedAt); err != nil {
			return nil, fmt.Errorf("scan latest batch row: %w", err)
		}
		assignNullInt(&l.LatencyMs, latency)
		assignNullInt(&l.PingLatencyMs, ping)
		out[monitorID] = append(out[monitorID], l)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// ListRecentHistoryForMonitors ""
// primaryModels[monitorID]
// + unnest() (monitor_id, model)
// () OVER (PARTITION BY monitor_id)
//
// [monitorID] -> []*ChannelMonitorHistoryEntry（
//
func (r *channelMonitorRepository) ListRecentHistoryForMonitors(
	ctx context.Context,
	ids []int64,
	primaryModels map[int64]string,
	perMonitorLimit int,
) (map[int64][]*service.ChannelMonitorHistoryEntry, error) {
	out := make(map[int64][]*service.ChannelMonitorHistoryEntry, len(ids))
	pairIDs, pairModels := buildMonitorModelPairs(ids, primaryModels)
	if len(pairIDs) == 0 {
		return out, nil
	}
	perMonitorLimit = clampTimelineLimit(perMonitorLimit)

	const q = `
		WITH targets AS (
		    SELECT unnest($1::bigint[]) AS monitor_id,
		           unnest($2::text[])   AS model
		),
		ranked AS (
		    SELECT h.monitor_id,
		           h.status,
		           h.latency_ms,
		           h.ping_latency_ms,
		           h.checked_at,
		           ROW_NUMBER() OVER (PARTITION BY h.monitor_id ORDER BY h.checked_at DESC) AS rn
		    FROM channel_monitor_histories h
		    JOIN targets t
		      ON t.monitor_id = h.monitor_id AND t.model = h.model
		)
		SELECT monitor_id, status, latency_ms, ping_latency_ms, checked_at
		FROM ranked
		WHERE rn <= $3
		ORDER BY monitor_id, checked_at DESC
	`
	rows, err := r.db.QueryContext(ctx, q, pq.Array(pairIDs), pq.Array(pairModels), perMonitorLimit)
	if err != nil {
		return nil, fmt.Errorf("query recent history batch: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var monitorID int64
		entry := &service.ChannelMonitorHistoryEntry{}
		var latency, ping sql.NullInt64
		if err := rows.Scan(&monitorID, &entry.Status, &latency, &ping, &entry.CheckedAt); err != nil {
			return nil, fmt.Errorf("scan recent history row: %w", err)
		}
		assignNullInt(&entry.LatencyMs, latency)
		assignNullInt(&entry.PingLatencyMs, ping)
		out[monitorID] = append(out[monitorID], entry)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// buildMonitorModelPairs (monitor_id, model)
//
func buildMonitorModelPairs(ids []int64, primaryModels map[int64]string) ([]int64, []string) {
	if len(ids) == 0 || len(primaryModels) == 0 {
		return nil, nil
	}
	pairIDs := make([]int64, 0, len(ids))
	pairModels := make([]string, 0, len(ids))
	for _, id := range ids {
		model, ok := primaryModels[id]
		if !ok || strings.TrimSpace(model) == "" {
			continue
		}
		pairIDs = append(pairIDs, id)
		pairModels = append(pairModels, model)
	}
	return pairIDs, pairModels
}

// timelineLimit*
//
const (
	timelineLimitMin = 1
	timelineLimitMax = 200
)

// clampTimelineLimit [timelineLimitMin, timelineLimitMax]，
func clampTimelineLimit(n int) int {
	if n < timelineLimitMin {
		return timelineLimitMin
	}
	if n > timelineLimitMax {
		return timelineLimitMax
	}
	return n
}

// ComputeAvailabilityForMonitors
// <= 30
func (r *channelMonitorRepository) ComputeAvailabilityForMonitors(ctx context.Context, ids []int64, windowDays int) (map[int64][]*service.ChannelMonitorAvailability, error) {
	out := make(map[int64][]*service.ChannelMonitorAvailability, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	if windowDays <= 0 {
		windowDays = 7
	}
	const q = `
		SELECT monitor_id,
		       model,
		       COUNT(*)                                                             AS total,
		       COUNT(*) FILTER (WHERE status IN ('operational','degraded'))         AS ok,
		       CASE WHEN COUNT(latency_ms) > 0
		            THEN SUM(latency_ms) FILTER (WHERE latency_ms IS NOT NULL)::float8 / COUNT(latency_ms)
		            ELSE NULL END                                                   AS avg_latency_ms
		FROM channel_monitor_histories
		WHERE monitor_id = ANY($1)
		  AND checked_at >= NOW() - ($2::int || ' days')::interval
		GROUP BY monitor_id, model
	`
	rows, err := r.db.QueryContext(ctx, q, pq.Array(ids), windowDays)
	if err != nil {
		return nil, fmt.Errorf("query availability batch: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var monitorID int64
		row := &service.ChannelMonitorAvailability{WindowDays: windowDays}
		var avgLatency sql.NullFloat64
		if err := rows.Scan(&monitorID, &row.Model, &row.TotalChecks, &row.OperationalChecks, &avgLatency); err != nil {
			return nil, fmt.Errorf("scan availability batch row: %w", err)
		}
		//
		//
		finalizeAvailabilityRow(row, avgLatency)
		out[monitorID] = append(out[monitorID], row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// ----------

// UpsertDailyRollupsFor [targetDate, targetDate+1d)）
// (monitor_id, model, bucket_date)
//   - (monitor_id, model, bucket_date) DO UPDATE
//   - $1::date
func (r *channelMonitorRepository) UpsertDailyRollupsFor(ctx context.Context, targetDate time.Time) (int64, error) {
	const q = `
		INSERT INTO channel_monitor_daily_rollups (
		    monitor_id, model, bucket_date,
		    total_checks, ok_count,
		    operational_count, degraded_count, failed_count, error_count,
		    sum_latency_ms, count_latency,
		    sum_ping_latency_ms, count_ping_latency,
		    computed_at
		)
		SELECT
		    monitor_id,
		    model,
		    $1::date AS bucket_date,
		    COUNT(*)                                                         AS total_checks,
		    COUNT(*) FILTER (WHERE status IN ('operational','degraded'))     AS ok_count,
		    COUNT(*) FILTER (WHERE status = 'operational')                   AS operational_count,
		    COUNT(*) FILTER (WHERE status = 'degraded')                      AS degraded_count,
		    COUNT(*) FILTER (WHERE status = 'failed')                        AS failed_count,
		    COUNT(*) FILTER (WHERE status = 'error')                         AS error_count,
		    COALESCE(SUM(latency_ms) FILTER (WHERE latency_ms IS NOT NULL), 0)             AS sum_latency_ms,
		    COUNT(latency_ms)                                                AS count_latency,
		    COALESCE(SUM(ping_latency_ms) FILTER (WHERE ping_latency_ms IS NOT NULL), 0)   AS sum_ping_latency_ms,
		    COUNT(ping_latency_ms)                                           AS count_ping_latency,
		    NOW()
		FROM channel_monitor_histories
		WHERE checked_at >= $1::date
		  AND checked_at <  ($1::date + INTERVAL '1 day')
		GROUP BY monitor_id, model
		ON CONFLICT (monitor_id, model, bucket_date) DO UPDATE SET
		    total_checks        = EXCLUDED.total_checks,
		    ok_count            = EXCLUDED.ok_count,
		    operational_count   = EXCLUDED.operational_count,
		    degraded_count      = EXCLUDED.degraded_count,
		    failed_count        = EXCLUDED.failed_count,
		    error_count         = EXCLUDED.error_count,
		    sum_latency_ms      = EXCLUDED.sum_latency_ms,
		    count_latency       = EXCLUDED.count_latency,
		    sum_ping_latency_ms = EXCLUDED.sum_ping_latency_ms,
		    count_ping_latency  = EXCLUDED.count_ping_latency,
		    computed_at         = NOW()
	`
	res, err := r.db.ExecContext(ctx, q, targetDate)
	if err != nil {
		return 0, fmt.Errorf("upsert daily rollups for %s: %w", targetDate.Format("2006-01-02"), err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("rows affected (upsert rollups): %w", err)
	}
	return n, nil
}

// DeleteRollupsBefore < beforeDate
func (r *channelMonitorRepository) DeleteRollupsBefore(ctx context.Context, beforeDate time.Time) (int64, error) {
	return deleteChannelMonitorBatched(ctx, r.db, channelMonitorPruneRollupSQL, beforeDate)
}

// channelMonitorPruneBatchSize
//
const channelMonitorPruneBatchSize = 5000

// channelMonitorPruneHistorySQL
const channelMonitorPruneHistorySQL = `
WITH batch AS (
    SELECT id FROM channel_monitor_histories
    WHERE checked_at < $1
    ORDER BY id
    LIMIT $2
)
DELETE FROM channel_monitor_histories
WHERE id IN (SELECT id FROM batch)
`

// channelMonitorPruneRollupSQL
//
const channelMonitorPruneRollupSQL = `
WITH batch AS (
    SELECT id FROM channel_monitor_daily_rollups
    WHERE bucket_date < $1::date
    ORDER BY id
    LIMIT $2
)
DELETE FROM channel_monitor_daily_rollups
WHERE id IN (SELECT id FROM batch)
`

// deleteChannelMonitorBatched
// cutoff
func deleteChannelMonitorBatched(ctx context.Context, db *sql.DB, query string, cutoff time.Time) (int64, error) {
	var total int64
	for {
		res, err := db.ExecContext(ctx, query, cutoff, channelMonitorPruneBatchSize)
		if err != nil {
			return total, fmt.Errorf("channel_monitor prune batch: %w", err)
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return total, fmt.Errorf("channel_monitor prune rows affected: %w", err)
		}
		total += affected
		if affected == 0 {
			break
		}
	}
	return total, nil
}

// LoadAggregationWatermark =1）。
// watermark
//   - (nil, nil)，
func (r *channelMonitorRepository) LoadAggregationWatermark(ctx context.Context) (*time.Time, error) {
	const q = `SELECT last_aggregated_date FROM channel_monitor_aggregation_watermark WHERE id = 1`
	var t sql.NullTime
	if err := r.db.QueryRowContext(ctx, q).Scan(&t); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("load aggregation watermark: %w", err)
	}
	if !t.Valid {
		return nil, nil
	}
	return &t.Time, nil
}

// UpdateAggregationWatermark =1）。
// $1::date
func (r *channelMonitorRepository) UpdateAggregationWatermark(ctx context.Context, date time.Time) error {
	const q = `
		INSERT INTO channel_monitor_aggregation_watermark (id, last_aggregated_date, updated_at)
		VALUES (1, $1::date, NOW())
		ON CONFLICT (id) DO UPDATE SET
		    last_aggregated_date = EXCLUDED.last_aggregated_date,
		    updated_at           = NOW()
	`
	if _, err := r.db.ExecContext(ctx, q, date); err != nil {
		return fmt.Errorf("update aggregation watermark: %w", err)
	}
	return nil
}

// ---------- helpers ----------

func entToServiceMonitor(row *dbent.ChannelMonitor) *service.ChannelMonitor {
	if row == nil {
		return nil
	}
	extras := row.ExtraModels
	if extras == nil {
		extras = []string{}
	}
	headers := row.ExtraHeaders
	if headers == nil {
		headers = map[string]string{}
	}
	out := &service.ChannelMonitor{
		ID:               row.ID,
		Name:             row.Name,
		Provider:         string(row.Provider),
		APIMode:          defaultAPIModeRepo(row.APIMode),
		Endpoint:         row.Endpoint,
		APIKey:           row.APIKeyEncrypted, // 仍为密文，service 层负责解密
		PrimaryModel:     row.PrimaryModel,
		ExtraModels:      extras,
		GroupName:        row.GroupName,
		Enabled:          row.Enabled,
		IntervalSeconds:  row.IntervalSeconds,
		LastCheckedAt:    row.LastCheckedAt,
		CreatedBy:        row.CreatedBy,
		CreatedAt:        row.CreatedAt,
		UpdatedAt:        row.UpdatedAt,
		ExtraHeaders:     headers,
		BodyOverrideMode: row.BodyOverrideMode,
		BodyOverride:     row.BodyOverride,
	}
	if row.TemplateID != nil {
		id := *row.TemplateID
		out.TemplateID = &id
	}
	return out
}

// emptyHeadersIfNilRepo
// repo
func emptyHeadersIfNilRepo(h map[string]string) map[string]string {
	if h == nil {
		return map[string]string{}
	}
	return h
}

// defaultBodyModeRepo
func defaultBodyModeRepo(mode string) string {
	if mode == "" {
		return "off"
	}
	return mode
}

func defaultAPIModeRepo(apiMode string) string {
	if apiMode == "" {
		return "chat_completions"
	}
	return apiMode
}

func emptySliceIfNil(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}
