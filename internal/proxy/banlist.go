package proxy

import (
	"net/netip"
	"sync/atomic"
	"time"

	"proxier/internal/model"
)

type banEntry struct {
	prefix  netip.Prefix
	expires int64 // unix seconds, 0 = never
}

type banSet struct {
	global  []banEntry
	perRule map[int64][]banEntry
}

// BanList is a lock-free, atomically swapped set of banned prefixes.
type BanList struct {
	set atomic.Pointer[banSet]
}

func NewBanList() *BanList {
	b := &BanList{}
	b.set.Store(&banSet{perRule: map[int64][]banEntry{}})
	return b
}

// Replace swaps in a new ban list. Invalid CIDRs are skipped.
func (b *BanList) Replace(bans []*model.Ban) {
	s := &banSet{perRule: map[int64][]banEntry{}}
	for _, ban := range bans {
		p, err := model.ParseCIDR(ban.CIDR)
		if err != nil {
			continue
		}
		e := banEntry{prefix: p}
		if ban.ExpiresAt != nil {
			e.expires = *ban.ExpiresAt
		}
		if ban.RuleID == nil {
			s.global = append(s.global, e)
		} else {
			s.perRule[*ban.RuleID] = append(s.perRule[*ban.RuleID], e)
		}
	}
	b.set.Store(s)
}

// Banned reports whether ip may not connect to the given rule.
func (b *BanList) Banned(ruleID int64, ip netip.Addr) bool {
	ip = ip.Unmap()
	s := b.set.Load()
	now := time.Now().Unix()
	match := func(list []banEntry) bool {
		for _, e := range list {
			if (e.expires == 0 || e.expires > now) && e.prefix.Contains(ip) {
				return true
			}
		}
		return false
	}
	return match(s.global) || match(s.perRule[ruleID])
}
