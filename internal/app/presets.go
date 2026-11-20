package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"proxier/internal/model"
	"proxier/internal/presets"
	"proxier/internal/store"
)

func (a *App) Presets(ctx context.Context) ([]*model.Preset, error) {
	custom, err := a.Store.ListPresets(ctx)
	if err != nil {
		return nil, err
	}
	return append(append([]*model.Preset{}, presets.Builtins()...), custom...), nil
}

func (a *App) findPreset(ctx context.Context, id string) (*model.Preset, error) {
	if p := presets.Builtin(id); p != nil {
		return p, nil
	}
	all, err := a.Store.ListPresets(ctx)
	if err != nil {
		return nil, err
	}
	for _, p := range all {
		if p.ID == id {
			return p, nil
		}
	}
	return nil, store.ErrNotFound
}

type ApplyPresetItem struct {
	Index           int  `json:"index"`
	Selected        bool `json:"selected"`
	ListenPortStart int  `json:"listen_port_start"`
	TargetPortStart int  `json:"target_port_start"`
}

type ApplyPresetRequest struct {
	TargetHost string            `json:"target_host"`
	ListenIP   string            `json:"listen_ip"`
	NamePrefix string            `json:"name_prefix"`
	Items      []ApplyPresetItem `json:"items"` // optional overrides; default = non-optional rules
}

// BuildPresetRules turns a preset + request into concrete rules (not saved).
func BuildPresetRules(p *model.Preset, req ApplyPresetRequest) ([]*model.Rule, error) {
	prefix := strings.TrimSpace(req.NamePrefix)
	if prefix == "" {
		prefix = p.Name
	}
	group := fmt.Sprintf("%s@%d", strings.TrimPrefix(strings.TrimPrefix(p.ID, "builtin:"), "custom:"), time.Now().Unix())
	items := map[int]ApplyPresetItem{}
	for _, it := range req.Items {
		items[it.Index] = it
	}
	var out []*model.Rule
	for i, pr := range p.Rules {
		it, has := items[i]
		selected := !pr.Optional
		if has {
			selected = it.Selected
		}
		if !selected {
			continue
		}
		width := max(pr.PortEnd-pr.PortStart, 0)
		listen := pr.PortStart
		target := pr.TargetPortStart
		if target == 0 {
			target = pr.PortStart
		}
		if has && it.ListenPortStart > 0 {
			listen = it.ListenPortStart
		}
		if has && it.TargetPortStart > 0 {
			target = it.TargetPortStart
		}
		r := &model.Rule{
			Name:            prefix + " · " + pr.Name,
			Enabled:         true,
			Protocol:        pr.Protocol,
			ListenIP:        strings.TrimSpace(req.ListenIP),
			ListenPortStart: listen,
			ListenPortEnd:   listen + width,
			TargetHost:      req.TargetHost,
			TargetPortStart: target,
			UDPIdleTimeoutS: pr.UDPTimeout,
			Notes:           pr.Notes,
			PresetGroup:     group,
		}
		r.Normalize()
		out = append(out, r)
	}
	if len(out) == 0 {
		return nil, model.FieldErrors{"items": "select at least one port"}
	}
	return out, nil
}

// ApplyPreset creates the rules of a preset. Either all rules are created or none.
func (a *App) ApplyPreset(ctx context.Context, presetID string, req ApplyPresetRequest) ([]*model.Rule, error) {
	p, err := a.findPreset(ctx, presetID)
	if err != nil {
		return nil, err
	}
	rules, err := BuildPresetRules(p, req)
	if err != nil {
		return nil, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	existing, err := a.Store.ListRules(ctx)
	if err != nil {
		return nil, err
	}
	for i, r := range rules {
		if err := a.checkRule(r, existing); err != nil {
			if fe, ok := err.(model.FieldErrors); ok {
				out := model.FieldErrors{}
				for k, v := range fe {
					if k == "target_host" || k == "listen_ip" {
						out[k] = v
					} else {
						out["items."+strconv.Itoa(i)] = r.Name + ": " + v
					}
				}
				return nil, out
			}
			return nil, err
		}
		existing = append(existing, r) // catch conflicts inside the preset itself
	}
	for _, r := range rules {
		if err := a.Store.CreateRule(ctx, r); err != nil {
			return nil, err
		}
	}
	a.Emit("info", "preset_applied", fmt.Sprintf("Preset %q applied: %d rule(s) → %s", p.Name, len(rules), req.TargetHost), nil, "")
	return rules, a.reloadLocked(ctx)
}

type PresetFromRulesRequest struct {
	Name        string  `json:"name"`
	Description string  `json:"description"`
	RuleIDs     []int64 `json:"rule_ids"`
}

func (a *App) PresetFromRules(ctx context.Context, req PresetFromRulesRequest) (*model.Preset, error) {
	req.Name = strings.TrimSpace(req.Name)
	fe := model.FieldErrors{}
	if req.Name == "" || len(req.Name) > 100 {
		fe["name"] = "required, at most 100 characters"
	}
	if len(req.RuleIDs) == 0 {
		fe["rule_ids"] = "select at least one rule"
	}
	if len(fe) > 0 {
		return nil, fe
	}
	p := &model.Preset{Name: req.Name, Description: strings.TrimSpace(req.Description)}
	for _, id := range req.RuleIDs {
		r, err := a.Store.GetRule(ctx, id)
		if err != nil {
			return nil, model.FieldErrors{"rule_ids": fmt.Sprintf("unknown rule %d", id)}
		}
		pr := model.PresetRule{
			Name: r.Name, Protocol: r.Protocol, PortStart: r.ListenPortStart, PortEnd: r.ListenPortEnd,
			Notes: r.Notes,
		}
		if r.TargetPortStart != r.ListenPortStart {
			pr.TargetPortStart = r.TargetPortStart
		}
		if r.UDPIdleTimeoutS != model.DefaultUDPIdleTimeout {
			pr.UDPTimeout = r.UDPIdleTimeoutS
		}
		p.Rules = append(p.Rules, pr)
	}
	if err := a.Store.CreatePreset(ctx, p); err != nil {
		return nil, err
	}
	a.Emit("info", "preset_created", fmt.Sprintf("Custom preset %q saved (%d rules)", p.Name, len(p.Rules)), nil, "")
	return p, nil
}

func (a *App) DeletePreset(ctx context.Context, id string) error {
	n, err := strconv.ParseInt(strings.TrimPrefix(id, "custom:"), 10, 64)
	if err != nil || !strings.HasPrefix(id, "custom:") {
		return model.FieldErrors{"id": "only custom presets can be deleted"}
	}
	return a.Store.DeletePreset(ctx, n)
}

// ---------- export / import ----------

type ExportDoc struct {
	Version    int             `json:"version"`
	ExportedAt int64           `json:"exported_at"`
	Rules      []*model.Rule   `json:"rules"`
	Bans       []*model.Ban    `json:"bans"`
	Presets    []*model.Preset `json:"presets"`
	Settings   *Settings       `json:"settings,omitempty"`
}

func (a *App) Export(ctx context.Context) (*ExportDoc, error) {
	rules, err := a.Store.ListRules(ctx)
	if err != nil {
		return nil, err
	}
	bans, err := a.Store.ListBans(ctx)
	if err != nil {
		return nil, err
	}
	ps, err := a.Store.ListPresets(ctx)
	if err != nil {
		return nil, err
	}
	s, err := a.GetSettings(ctx)
	if err != nil {
		return nil, err
	}
	return &ExportDoc{Version: 1, ExportedAt: time.Now().Unix(), Rules: rules, Bans: bans, Presets: ps, Settings: &s}, nil
}

type ImportResult struct {
	Rules         int `json:"rules"`
	RulesDisabled int `json:"rules_disabled"` // imported disabled because of conflicts
	Bans          int `json:"bans"`
	Presets       int `json:"presets"`
}

// Import loads an export document. mode "replace" wipes rules, bans and custom
// presets first; "merge" adds to what exists. Rules that would conflict are
// imported disabled.
func (a *App) Import(ctx context.Context, doc *ExportDoc, mode string) (*ImportResult, error) {
	if doc.Version != 1 {
		return nil, fmt.Errorf("unsupported export version %d", doc.Version)
	}
	for i, r := range doc.Rules {
		r.Normalize()
		if err := r.Validate(); err != nil {
			return nil, fmt.Errorf("rule %d (%q): %v", i, r.Name, err)
		}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if mode == "replace" {
		tx, err := a.Store.DB.BeginTx(ctx, nil)
		if err != nil {
			return nil, err
		}
		for _, q := range []string{"DELETE FROM bans", "DELETE FROM rules", "DELETE FROM presets"} {
			if _, err := tx.ExecContext(ctx, q); err != nil {
				tx.Rollback()
				return nil, err
			}
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		// Release listeners so conflict checks see the ports as free.
		a.Proxy.Apply(nil)
	}
	res := &ImportResult{}
	idMap := map[int64]int64{}
	existing, err := a.Store.ListRules(ctx)
	if err != nil {
		return nil, err
	}
	for _, r := range doc.Rules {
		oldID := r.ID
		r.ID = 0
		if r.Enabled && a.checkRule(r, existing) != nil {
			r.Enabled = false
			res.RulesDisabled++
		}
		if err := a.Store.CreateRule(ctx, r); err != nil {
			return nil, err
		}
		idMap[oldID] = r.ID
		existing = append(existing, r)
		res.Rules++
	}
	for _, b := range doc.Bans {
		if _, err := model.ParseCIDR(b.CIDR); err != nil {
			continue
		}
		if b.RuleID != nil {
			nid, ok := idMap[*b.RuleID]
			if !ok {
				continue
			}
			b.RuleID = &nid
		}
		if err := a.Store.CreateBan(ctx, b); err != nil {
			return nil, err
		}
		res.Bans++
	}
	for _, p := range doc.Presets {
		if p.Builtin || p.Name == "" || len(p.Rules) == 0 {
			continue
		}
		if err := a.Store.CreatePreset(ctx, p); err != nil {
			return nil, err
		}
		res.Presets++
	}
	if doc.Settings != nil {
		if err := a.SaveSettings(ctx, *doc.Settings); err != nil {
			a.Log.Warn("import settings", "err", err)
		}
	}
	a.Emit("warn", "config_imported", fmt.Sprintf("Configuration imported (%s): %d rules, %d bans, %d presets", mode, res.Rules, res.Bans, res.Presets), nil, "")
	return res, a.reloadLocked(ctx)
}

// MarshalExport is a helper for the CLI/API.
func MarshalExport(d *ExportDoc) ([]byte, error) { return json.MarshalIndent(d, "", "  ") }
