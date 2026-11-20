package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"proxier/internal/model"
)

// ---- bans ----

func (s *Store) ListBans(ctx context.Context) ([]*model.Ban, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, cidr, rule_id, reason, created_at, expires_at FROM bans
		WHERE expires_at IS NULL OR expires_at > ? ORDER BY id DESC`, now())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*model.Ban{}
	for rows.Next() {
		var b model.Ban
		var ruleID, exp sql.NullInt64
		if err := rows.Scan(&b.ID, &b.CIDR, &ruleID, &b.Reason, &b.CreatedAt, &exp); err != nil {
			return nil, err
		}
		if ruleID.Valid {
			b.RuleID = &ruleID.Int64
		}
		if exp.Valid {
			b.ExpiresAt = &exp.Int64
		}
		out = append(out, &b)
	}
	return out, rows.Err()
}

func (s *Store) CreateBan(ctx context.Context, b *model.Ban) error {
	b.CreatedAt = now()
	res, err := s.DB.ExecContext(ctx, `INSERT INTO bans (cidr, rule_id, reason, created_at, expires_at) VALUES (?,?,?,?,?)`,
		b.CIDR, b.RuleID, b.Reason, b.CreatedAt, b.ExpiresAt)
	if err != nil {
		return err
	}
	b.ID, err = res.LastInsertId()
	return err
}

func (s *Store) DeleteBan(ctx context.Context, id int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM bans WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// PurgeExpiredBans deletes expired bans and reports how many were removed.
func (s *Store) PurgeExpiredBans(ctx context.Context) (int64, error) {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM bans WHERE expires_at IS NOT NULL AND expires_at <= ?`, now())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// ---- custom presets ----

func (s *Store) ListPresets(ctx context.Context) ([]*model.Preset, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, name, description, rules_json, created_at FROM presets ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*model.Preset{}
	for rows.Next() {
		var p model.Preset
		var id int64
		var rj string
		if err := rows.Scan(&id, &p.Name, &p.Description, &rj, &p.CreatedAt); err != nil {
			return nil, err
		}
		p.ID = "custom:" + strconv.FormatInt(id, 10)
		if err := json.Unmarshal([]byte(rj), &p.Rules); err != nil {
			return nil, err
		}
		out = append(out, &p)
	}
	return out, rows.Err()
}

func (s *Store) CreatePreset(ctx context.Context, p *model.Preset) error {
	rj, err := json.Marshal(p.Rules)
	if err != nil {
		return err
	}
	p.CreatedAt = now()
	res, err := s.DB.ExecContext(ctx, `INSERT INTO presets (name, description, rules_json, created_at) VALUES (?,?,?,?)`,
		p.Name, p.Description, string(rj), p.CreatedAt)
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	p.ID = "custom:" + strconv.FormatInt(id, 10)
	return err
}

func (s *Store) DeletePreset(ctx context.Context, id int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM presets WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ---- settings ----

func (s *Store) GetSettings(ctx context.Context) (map[string]string, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT key, value FROM settings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}

func (s *Store) SetSettings(ctx context.Context, kv map[string]string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for k, v := range kv {
		if _, err := tx.ExecContext(ctx, `INSERT INTO settings(key, value) VALUES (?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, k, v); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ---- admin & sessions ----

func (s *Store) AdminPasswordHash(ctx context.Context) (string, error) {
	var h string
	err := s.DB.QueryRowContext(ctx, `SELECT password_hash FROM admin WHERE id=1`).Scan(&h)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return h, err
}

func (s *Store) SetAdminPasswordHash(ctx context.Context, hash string) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO admin(id, password_hash, updated_at) VALUES (1,?,?)
		ON CONFLICT(id) DO UPDATE SET password_hash=excluded.password_hash, updated_at=excluded.updated_at`, hash, now())
	return err
}

// CreateAdminIfMissing sets the admin password only if no admin exists yet.
// It reports whether the admin was created.
func (s *Store) CreateAdminIfMissing(ctx context.Context, hash string) (bool, error) {
	res, err := s.DB.ExecContext(ctx, `INSERT OR IGNORE INTO admin(id, password_hash, updated_at) VALUES (1,?,?)`, hash, now())
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

type Session struct {
	TokenHash string
	CreatedAt int64
	ExpiresAt int64
	LastIP    string
	UserAgent string
}

func (s *Store) CreateSession(ctx context.Context, sess *Session) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO sessions(token_hash, created_at, expires_at, last_ip, user_agent) VALUES (?,?,?,?,?)`,
		sess.TokenHash, sess.CreatedAt, sess.ExpiresAt, sess.LastIP, sess.UserAgent)
	return err
}

func (s *Store) GetSession(ctx context.Context, tokenHash string) (*Session, error) {
	var sess Session
	err := s.DB.QueryRowContext(ctx, `SELECT token_hash, created_at, expires_at, last_ip, user_agent FROM sessions WHERE token_hash=?`, tokenHash).
		Scan(&sess.TokenHash, &sess.CreatedAt, &sess.ExpiresAt, &sess.LastIP, &sess.UserAgent)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &sess, err
}

func (s *Store) TouchSession(ctx context.Context, tokenHash string, expiresAt int64, ip string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE sessions SET expires_at=?, last_ip=? WHERE token_hash=?`, expiresAt, ip, tokenHash)
	return err
}

func (s *Store) DeleteSession(ctx context.Context, tokenHash string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash=?`, tokenHash)
	return err
}

// DeleteSessionsExcept removes every session but the given one ("" removes all).
func (s *Store) DeleteSessionsExcept(ctx context.Context, keepHash string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash != ?`, keepHash)
	return err
}

func (s *Store) PurgeExpiredSessions(ctx context.Context) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at <= ?`, now())
	return err
}

// ---- events ----

func (s *Store) InsertEvent(ctx context.Context, e *model.Event) error {
	meta := ""
	if len(e.Meta) > 0 {
		b, _ := json.Marshal(e.Meta)
		meta = string(b)
	}
	res, err := s.DB.ExecContext(ctx, `INSERT INTO events(ts, level, type, rule_id, client_ip, message, meta_json) VALUES (?,?,?,?,?,?,?)`,
		e.TS, e.Level, e.Type, e.RuleID, e.ClientIP, e.Message, meta)
	if err != nil {
		return err
	}
	e.ID, err = res.LastInsertId()
	return err
}

type EventQuery struct {
	Level  string
	Type   string
	RuleID *int64
	Q      string
	Before int64 // event id cursor, 0 = newest
	Limit  int
}

func (s *Store) ListEvents(ctx context.Context, q EventQuery) ([]*model.Event, error) {
	var where []string
	var args []any
	if q.Level != "" {
		where = append(where, "level = ?")
		args = append(args, q.Level)
	}
	if q.Type != "" {
		where = append(where, "type = ?")
		args = append(args, q.Type)
	}
	if q.RuleID != nil {
		where = append(where, "rule_id = ?")
		args = append(args, *q.RuleID)
	}
	if q.Q != "" {
		where = append(where, "(message LIKE ? ESCAPE '\\' OR client_ip LIKE ? ESCAPE '\\')")
		like := "%" + escapeLike(q.Q) + "%"
		args = append(args, like, like)
	}
	if q.Before > 0 {
		where = append(where, "id < ?")
		args = append(args, q.Before)
	}
	if q.Limit <= 0 || q.Limit > 500 {
		q.Limit = 100
	}
	sqlStr := `SELECT id, ts, level, type, rule_id, client_ip, message, meta_json FROM events`
	if len(where) > 0 {
		sqlStr += " WHERE " + strings.Join(where, " AND ")
	}
	sqlStr += " ORDER BY id DESC LIMIT ?"
	args = append(args, q.Limit)
	rows, err := s.DB.QueryContext(ctx, sqlStr, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*model.Event{}
	for rows.Next() {
		var e model.Event
		var ruleID sql.NullInt64
		var meta string
		if err := rows.Scan(&e.ID, &e.TS, &e.Level, &e.Type, &ruleID, &e.ClientIP, &e.Message, &meta); err != nil {
			return nil, err
		}
		if ruleID.Valid {
			e.RuleID = &ruleID.Int64
		}
		if meta != "" {
			_ = json.Unmarshal([]byte(meta), &e.Meta)
		}
		out = append(out, &e)
	}
	return out, rows.Err()
}

func escapeLike(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

func (s *Store) PurgeEventsBefore(ctx context.Context, cutoff time.Time) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM events WHERE ts < ?`, cutoff.UnixMilli())
	return err
}

// ---- traffic ----

type TrafficRow struct {
	RuleID      int64
	TS          int64
	BytesIn     uint64
	BytesOut    uint64
	ConnsOpened uint64
	PeakConns   int64
}

func (s *Store) InsertTrafficMinutes(ctx context.Context, rows []TrafficRow) error {
	if len(rows) == 0 {
		return nil
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO traffic_minute(rule_id, ts, bytes_in, bytes_out, conns_opened, peak_conns)
		VALUES (?,?,?,?,?,?) ON CONFLICT(rule_id, ts) DO UPDATE SET
		bytes_in = bytes_in + excluded.bytes_in, bytes_out = bytes_out + excluded.bytes_out,
		conns_opened = conns_opened + excluded.conns_opened, peak_conns = MAX(peak_conns, excluded.peak_conns)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, r := range rows {
		if _, err := stmt.ExecContext(ctx, r.RuleID, r.TS, int64(r.BytesIn), int64(r.BytesOut), int64(r.ConnsOpened), r.PeakConns); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// RollupTraffic aggregates minute buckets older than minuteKeep into hourly
// buckets, then deletes old minute and hour rows.
func (s *Store) RollupTraffic(ctx context.Context, minuteKeep, hourKeep time.Duration) error {
	cut := time.Now().Add(-minuteKeep).Truncate(time.Hour).Unix()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO traffic_hour(rule_id, ts, bytes_in, bytes_out, conns_opened, peak_conns)
		SELECT rule_id, (ts / 3600) * 3600 AS h, SUM(bytes_in), SUM(bytes_out), SUM(conns_opened), MAX(peak_conns)
		FROM traffic_minute WHERE ts < ? GROUP BY rule_id, h
		ON CONFLICT(rule_id, ts) DO UPDATE SET
		bytes_in = bytes_in + excluded.bytes_in, bytes_out = bytes_out + excluded.bytes_out,
		conns_opened = conns_opened + excluded.conns_opened, peak_conns = MAX(peak_conns, excluded.peak_conns)`, cut); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM traffic_minute WHERE ts < ?`, cut); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM traffic_hour WHERE ts < ?`, time.Now().Add(-hourKeep).Unix()); err != nil {
		return err
	}
	return tx.Commit()
}

// QueryTraffic returns buckets of bucketSec seconds between from and to (unix seconds).
// ruleID nil = all rules summed. Minute and hour tables are merged so that
// ranges spanning the rollup boundary are complete.
func (s *Store) QueryTraffic(ctx context.Context, ruleID *int64, from, to int64, bucketSec int64) ([]model.TrafficPoint, error) {
	filter := ""
	args := []any{}
	if ruleID != nil {
		filter = " AND rule_id = ?"
	}
	q := `SELECT (ts / ?) * ? AS b, SUM(bytes_in), SUM(bytes_out), SUM(conns_opened), MAX(peak_conns) FROM (
			SELECT ts, bytes_in, bytes_out, conns_opened, peak_conns FROM traffic_minute WHERE ts >= ? AND ts < ?` + filter + `
			UNION ALL
			SELECT ts, bytes_in, bytes_out, conns_opened, peak_conns FROM traffic_hour WHERE ts >= ? AND ts < ?` + filter + `
		) GROUP BY b ORDER BY b`
	args = append(args, bucketSec, bucketSec, from, to)
	if ruleID != nil {
		args = append(args, *ruleID)
	}
	args = append(args, from, to)
	if ruleID != nil {
		args = append(args, *ruleID)
	}
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.TrafficPoint{}
	for rows.Next() {
		var p model.TrafficPoint
		var bi, bo, co int64
		if err := rows.Scan(&p.TS, &bi, &bo, &co, &p.PeakConns); err != nil {
			return nil, err
		}
		p.BytesIn, p.BytesOut, p.ConnsOpened = uint64(bi), uint64(bo), uint64(co)
		out = append(out, p)
	}
	return out, rows.Err()
}

// TrafficTotals returns all-time byte totals per rule.
func (s *Store) TrafficTotals(ctx context.Context) (map[int64][2]uint64, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT rule_id, SUM(bi), SUM(bo) FROM (
		SELECT rule_id, SUM(bytes_in) bi, SUM(bytes_out) bo FROM traffic_minute GROUP BY rule_id
		UNION ALL
		SELECT rule_id, SUM(bytes_in), SUM(bytes_out) FROM traffic_hour GROUP BY rule_id) GROUP BY rule_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64][2]uint64{}
	for rows.Next() {
		var id, bi, bo int64
		if err := rows.Scan(&id, &bi, &bo); err != nil {
			return nil, err
		}
		out[id] = [2]uint64{uint64(bi), uint64(bo)}
	}
	return out, rows.Err()
}

func (s *Store) DeleteRuleTraffic(ctx context.Context, ruleID int64) error {
	if _, err := s.DB.ExecContext(ctx, `DELETE FROM traffic_minute WHERE rule_id=?`, ruleID); err != nil {
		return err
	}
	_, err := s.DB.ExecContext(ctx, `DELETE FROM traffic_hour WHERE rule_id=?`, ruleID)
	return err
}
