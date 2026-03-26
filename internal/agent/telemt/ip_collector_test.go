package telemt

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIPCollectorAccumulatesAcrossPolls(t *testing.T) {
	c := NewIPCollector()
	c.Update([]UserActiveIPs{{Username: "alice", ActiveIPs: []string{"1.1.1.1"}}})
	c.Update([]UserActiveIPs{{Username: "alice", ActiveIPs: []string{"2.2.2.2"}}})
	result := c.Flush()
	require.Len(t, result, 1)
	assert.Equal(t, "alice", result[0].Username)
	assert.ElementsMatch(t, []string{"1.1.1.1", "2.2.2.2"}, result[0].ActiveIPs)
}

func TestIPCollectorFlushClearsState(t *testing.T) {
	c := NewIPCollector()
	c.Update([]UserActiveIPs{{Username: "alice", ActiveIPs: []string{"1.1.1.1"}}})
	first := c.Flush()
	require.Len(t, first, 1)

	second := c.Flush()
	assert.Nil(t, second)
}

func TestIPCollectorMultipleUsers(t *testing.T) {
	c := NewIPCollector()
	c.Update([]UserActiveIPs{
		{Username: "alice", ActiveIPs: []string{"1.1.1.1"}},
	})
	c.Update([]UserActiveIPs{
		{Username: "bob", ActiveIPs: []string{"2.2.2.2"}},
	})
	result := c.Flush()
	require.Len(t, result, 2)
	// Result is sorted by username.
	assert.Equal(t, "alice", result[0].Username)
	assert.Equal(t, []string{"1.1.1.1"}, result[0].ActiveIPs)
	assert.Equal(t, "bob", result[1].Username)
	assert.Equal(t, []string{"2.2.2.2"}, result[1].ActiveIPs)
}

func TestIPCollectorDeduplicatesIPs(t *testing.T) {
	c := NewIPCollector()
	c.Update([]UserActiveIPs{{Username: "alice", ActiveIPs: []string{"1.1.1.1"}}})
	c.Update([]UserActiveIPs{{Username: "alice", ActiveIPs: []string{"1.1.1.1"}}})
	result := c.Flush()
	require.Len(t, result, 1)
	assert.Equal(t, []string{"1.1.1.1"}, result[0].ActiveIPs)
}
