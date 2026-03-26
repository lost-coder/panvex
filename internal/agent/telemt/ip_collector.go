package telemt

import (
	"sort"
	"sync"
)

// UserActiveIPs holds a username and the set of IPs observed active for that user.
type UserActiveIPs struct {
	Username  string   `json:"username"`
	ActiveIPs []string `json:"active_ips"`
}

// IPCollector accumulates active IPs per user across multiple polls.
// Call Update() on every poll cycle and Flush() when ready to ship a snapshot.
type IPCollector struct {
	mu       sync.Mutex
	observed map[string]map[string]struct{} // user → set of IPs
}

// NewIPCollector returns a ready-to-use IPCollector.
func NewIPCollector() *IPCollector {
	return &IPCollector{
		observed: make(map[string]map[string]struct{}),
	}
}

// Update merges newly observed IPs into the accumulated set. Thread-safe.
func (c *IPCollector) Update(users []UserActiveIPs) {
	c.mu.Lock()
	defer c.mu.Unlock()

	for _, u := range users {
		if _, ok := c.observed[u.Username]; !ok {
			c.observed[u.Username] = make(map[string]struct{})
		}
		for _, ip := range u.ActiveIPs {
			c.observed[u.Username][ip] = struct{}{}
		}
	}
}

// Flush returns the accumulated IPs per user (sorted by username, each user's
// IPs sorted), then resets the collector. Returns nil if nothing was observed.
func (c *IPCollector) Flush() []UserActiveIPs {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.observed) == 0 {
		return nil
	}

	// Collect and sort usernames for deterministic output.
	usernames := make([]string, 0, len(c.observed))
	for u := range c.observed {
		usernames = append(usernames, u)
	}
	sort.Strings(usernames)

	result := make([]UserActiveIPs, 0, len(usernames))
	for _, u := range usernames {
		ips := make([]string, 0, len(c.observed[u]))
		for ip := range c.observed[u] {
			ips = append(ips, ip)
		}
		sort.Strings(ips)
		result = append(result, UserActiveIPs{
			Username:  u,
			ActiveIPs: ips,
		})
	}

	// Reset state.
	c.observed = make(map[string]map[string]struct{})

	return result
}
