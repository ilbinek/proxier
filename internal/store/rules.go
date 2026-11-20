package store

import (
	"context"
	"database/sql"
	"errors"

	"proxier/internal/model"
)

const ruleCols = `id, name, enabled, protocol, listen_ip, listen_port_start, listen_port_end,
	target_host, target_port_start, udp_idle_timeout_s, notes, preset_group, created_at, updated_at`

type scanner interface{ Scan(dest ...any) error }

func scanRule(sc scanner) (*model.Rule, error) {
	var r model.Rule
	var proto string
	err := sc.Scan(&r.ID, &r.Name, &r.Enabled, &proto, &r.ListenIP, &r.ListenPortStart, &r.ListenPortEnd,
		&r.TargetHost, &r.TargetPortStart, &r.UDPIdleTimeoutS, &r.Notes, &r.PresetGroup, &r.CreatedAt, &r.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	r.Protocol = model.Protocol(proto)
	return &r, err
}

func (s *Store) ListRules(ctx context.Context) ([]*model.Rule, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+ruleCols+` FROM rules ORDER BY listen_port_start, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*model.Rule{}
	for rows.Next() {
		r, err := scanRule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) GetRule(ctx context.Context, id int64) (*model.Rule, error) {
	return scanRule(s.DB.QueryRowContext(ctx, `SELECT `+ruleCols+` FROM rules WHERE id = ?`, id))
}

func (s *Store) CreateRule(ctx context.Context, r *model.Rule) error {
	t := now()
	r.CreatedAt, r.UpdatedAt = t, t
	res, err := s.DB.ExecContext(ctx, `INSERT INTO rules (name, enabled, protocol, listen_ip, listen_port_start, listen_port_end,
		target_host, target_port_start, udp_idle_timeout_s, notes, preset_group, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		r.Name, r.Enabled, string(r.Protocol), r.ListenIP, r.ListenPortStart, r.ListenPortEnd,
		r.TargetHost, r.TargetPortStart, r.UDPIdleTimeoutS, r.Notes, r.PresetGroup, r.CreatedAt, r.UpdatedAt)
	if err != nil {
		return err
	}
	r.ID, err = res.LastInsertId()
	return err
}

func (s *Store) UpdateRule(ctx context.Context, r *model.Rule) error {
	r.UpdatedAt = now()
	res, err := s.DB.ExecContext(ctx, `UPDATE rules SET name=?, enabled=?, protocol=?, listen_ip=?, listen_port_start=?,
		listen_port_end=?, target_host=?, target_port_start=?, udp_idle_timeout_s=?, notes=?, preset_group=?, updated_at=?
		WHERE id=?`,
		r.Name, r.Enabled, string(r.Protocol), r.ListenIP, r.ListenPortStart, r.ListenPortEnd,
		r.TargetHost, r.TargetPortStart, r.UDPIdleTimeoutS, r.Notes, r.PresetGroup, r.UpdatedAt, r.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) SetRuleEnabled(ctx context.Context, id int64, enabled bool) error {
	res, err := s.DB.ExecContext(ctx, `UPDATE rules SET enabled=?, updated_at=? WHERE id=?`, enabled, now(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeleteRule(ctx context.Context, id int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM rules WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
