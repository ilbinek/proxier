// Package model contains the domain types shared by the store, proxy engine and API.
package model

import (
	"fmt"
	"net/netip"
	"strings"
)

type Protocol string

const (
	ProtoTCP  Protocol = "tcp"
	ProtoUDP  Protocol = "udp"
	ProtoBoth Protocol = "both"
)

func (p Protocol) HasTCP() bool { return p == ProtoTCP || p == ProtoBoth }
func (p Protocol) HasUDP() bool { return p == ProtoUDP || p == ProtoBoth }

// MaxRangeSize caps how many ports a single rule may listen on.
const MaxRangeSize = 1000

const DefaultUDPIdleTimeout = 120

type Rule struct {
	ID              int64    `json:"id"`
	Name            string   `json:"name"`
	Enabled         bool     `json:"enabled"`
	Protocol        Protocol `json:"protocol"`
	ListenIP        string   `json:"listen_ip"` // "" = all interfaces (dual-stack)
	ListenPortStart int      `json:"listen_port_start"`
	ListenPortEnd   int      `json:"listen_port_end"`
	TargetHost      string   `json:"target_host"`
	TargetPortStart int      `json:"target_port_start"`
	UDPIdleTimeoutS int      `json:"udp_idle_timeout_s"`
	Notes           string   `json:"notes"`
	PresetGroup     string   `json:"preset_group"`
	CreatedAt       int64    `json:"created_at"`
	UpdatedAt       int64    `json:"updated_at"`
}

// PortCount is the number of ports in the listen range.
func (r *Rule) PortCount() int { return r.ListenPortEnd - r.ListenPortStart + 1 }

// TargetPortFor maps a listen port to its target port.
func (r *Rule) TargetPortFor(listenPort int) int {
	return r.TargetPortStart + (listenPort - r.ListenPortStart)
}

// TargetPortEnd is the last target port of the range.
func (r *Rule) TargetPortEnd() int { return r.TargetPortFor(r.ListenPortEnd) }

// FieldErrors maps a JSON field name to a human readable problem.
type FieldErrors map[string]string

func (fe FieldErrors) Error() string {
	parts := make([]string, 0, len(fe))
	for k, v := range fe {
		parts = append(parts, k+": "+v)
	}
	return strings.Join(parts, "; ")
}

// Normalize fills defaults and trims fields.
func (r *Rule) Normalize() {
	r.Name = strings.TrimSpace(r.Name)
	r.ListenIP = strings.TrimSpace(r.ListenIP)
	r.TargetHost = strings.TrimSpace(r.TargetHost)
	r.Notes = strings.TrimSpace(r.Notes)
	r.Protocol = Protocol(strings.ToLower(string(r.Protocol)))
	if r.ListenPortEnd == 0 {
		r.ListenPortEnd = r.ListenPortStart
	}
	if r.TargetPortStart == 0 {
		r.TargetPortStart = r.ListenPortStart
	}
	if r.UDPIdleTimeoutS == 0 {
		r.UDPIdleTimeoutS = DefaultUDPIdleTimeout
	}
	// Strip brackets from IPv6 targets like "[::1]".
	r.TargetHost = strings.TrimSuffix(strings.TrimPrefix(r.TargetHost, "["), "]")
}

// Validate checks the rule in isolation (no conflict checks).
func (r *Rule) Validate() error {
	fe := FieldErrors{}
	if r.Name == "" {
		fe["name"] = "required"
	} else if len(r.Name) > 100 {
		fe["name"] = "at most 100 characters"
	}
	switch r.Protocol {
	case ProtoTCP, ProtoUDP, ProtoBoth:
	default:
		fe["protocol"] = "must be tcp, udp or both"
	}
	if r.ListenIP != "" {
		if _, err := netip.ParseAddr(r.ListenIP); err != nil {
			fe["listen_ip"] = "must be an IP address or empty for all interfaces"
		}
	}
	validPort := func(p int) bool { return p >= 1 && p <= 65535 }
	if !validPort(r.ListenPortStart) {
		fe["listen_port_start"] = "must be 1-65535"
	}
	if !validPort(r.ListenPortEnd) {
		fe["listen_port_end"] = "must be 1-65535"
	} else if r.ListenPortEnd < r.ListenPortStart {
		fe["listen_port_end"] = "must be >= start port"
	} else if r.PortCount() > MaxRangeSize {
		fe["listen_port_end"] = fmt.Sprintf("range too large (max %d ports)", MaxRangeSize)
	}
	if !validPort(r.TargetPortStart) {
		fe["target_port_start"] = "must be 1-65535"
	} else if _, ok := fe["listen_port_end"]; !ok && r.TargetPortEnd() > 65535 {
		fe["target_port_start"] = "target range would exceed port 65535"
	}
	if r.TargetHost == "" {
		fe["target_host"] = "required"
	} else if !validHost(r.TargetHost) {
		fe["target_host"] = "must be an IP address or hostname"
	}
	if r.UDPIdleTimeoutS < 5 || r.UDPIdleTimeoutS > 86400 {
		fe["udp_idle_timeout_s"] = "must be 5-86400 seconds"
	}
	if len(r.Notes) > 2000 {
		fe["notes"] = "at most 2000 characters"
	}
	if len(fe) > 0 {
		return fe
	}
	return nil
}

func validHost(h string) bool {
	if _, err := netip.ParseAddr(h); err == nil {
		return true
	}
	if len(h) > 253 {
		return false
	}
	for label := range strings.SplitSeq(strings.TrimSuffix(h, "."), ".") {
		if label == "" || len(label) > 63 {
			return false
		}
		for i, c := range label {
			ok := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_'
			if !ok || (c == '-' && (i == 0 || i == len(label)-1)) {
				return false
			}
		}
	}
	return true
}

type Ban struct {
	ID        int64  `json:"id"`
	CIDR      string `json:"cidr"`
	RuleID    *int64 `json:"rule_id"` // nil = global
	Reason    string `json:"reason"`
	CreatedAt int64  `json:"created_at"`
	ExpiresAt *int64 `json:"expires_at"`
}

// ParseCIDR accepts "1.2.3.4", "1.2.3.0/24", "2001:db8::/32" and returns a masked prefix.
func ParseCIDR(s string) (netip.Prefix, error) {
	s = strings.TrimSpace(s)
	if strings.Contains(s, "/") {
		p, err := netip.ParsePrefix(s)
		if err != nil {
			return netip.Prefix{}, err
		}
		return p.Masked(), nil
	}
	a, err := netip.ParseAddr(s)
	if err != nil {
		return netip.Prefix{}, err
	}
	a = a.Unmap()
	return netip.PrefixFrom(a, a.BitLen()), nil
}

// PresetRule is one rule template within a preset.
type PresetRule struct {
	Name            string   `json:"name"`
	Protocol        Protocol `json:"protocol"`
	PortStart       int      `json:"port_start"`                  // listen port (start)
	PortEnd         int      `json:"port_end"`                    // listen port (end)
	TargetPortStart int      `json:"target_port_start,omitempty"` // 0 = same as PortStart
	Optional        bool     `json:"optional"`                    // not selected by default when applying
	SamePort        bool     `json:"same_port"`                   // listen port must equal target port (e.g. TS3 file transfer)
	Notes           string   `json:"notes,omitempty"`
	UDPTimeout      int      `json:"udp_idle_timeout_s,omitempty"`
}

type Preset struct {
	ID          string       `json:"id"` // "builtin:ts3" or "custom:12"
	Name        string       `json:"name"`
	Description string       `json:"description"`
	Builtin     bool         `json:"builtin"`
	Rules       []PresetRule `json:"rules"`
	Hint        string       `json:"hint,omitempty"`
	CreatedAt   int64        `json:"created_at,omitempty"`
}

type Event struct {
	ID       int64          `json:"id"`
	TS       int64          `json:"ts"` // unix millis
	Level    string         `json:"level"`
	Type     string         `json:"type"`
	RuleID   *int64         `json:"rule_id"`
	ClientIP string         `json:"client_ip"`
	Message  string         `json:"message"`
	Meta     map[string]any `json:"meta,omitempty"`
}

type TrafficPoint struct {
	TS          int64  `json:"ts"` // unix seconds, bucket start
	BytesIn     uint64 `json:"bytes_in"`
	BytesOut    uint64 `json:"bytes_out"`
	ConnsOpened uint64 `json:"conns_opened"`
	PeakConns   int64  `json:"peak_conns"`
}
