package telemt

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const sampleMetrics = `# HELP telemt_user_octets_from_client Total octets received from client
# TYPE telemt_user_octets_from_client counter
telemt_user_octets_from_client{user="alice"} 1234567
telemt_user_octets_from_client{user="bob"} 999
# HELP telemt_user_octets_to_client Total octets sent to client
# TYPE telemt_user_octets_to_client counter
telemt_user_octets_to_client{user="alice"} 7654321
telemt_user_octets_to_client{user="bob"} 111
# HELP telemt_user_connections_current Current connections per user
# TYPE telemt_user_connections_current gauge
telemt_user_connections_current{user="alice"} 3
telemt_user_connections_current{user="bob"} 1
# HELP telemt_user_unique_ips_current Current unique IPs per user
# TYPE telemt_user_unique_ips_current gauge
telemt_user_unique_ips_current{user="alice"} 2
telemt_user_unique_ips_current{user="bob"} 1
# HELP telemt_server_octets_total Server-level metric
# TYPE telemt_server_octets_total counter
telemt_server_octets_total 9999999
`

func TestParseUserMetricsExtractsPerUserCounters(t *testing.T) {
	result := ParseUserMetrics(sampleMetrics)

	require.Len(t, result, 2)

	alice, ok := result["alice"]
	require.True(t, ok, "missing user alice")
	assert.Equal(t, uint64(1234567), alice.OctetsFromClient)
	assert.Equal(t, uint64(7654321), alice.OctetsToClient)
	assert.Equal(t, 3, alice.CurrentConnections)
	assert.Equal(t, 2, alice.UniqueIPsCurrent)

	bob, ok := result["bob"]
	require.True(t, ok, "missing user bob")
	assert.Equal(t, uint64(999), bob.OctetsFromClient)
	assert.Equal(t, uint64(111), bob.OctetsToClient)
	assert.Equal(t, 1, bob.CurrentConnections)
	assert.Equal(t, 1, bob.UniqueIPsCurrent)
}

func TestParseUserMetricsHandlesEmptyInput(t *testing.T) {
	result := ParseUserMetrics("")
	require.NotNil(t, result)
	assert.Empty(t, result)
}

func TestParseUserMetricsIgnoresNonUserMetrics(t *testing.T) {
	input := `telemt_server_octets_total 9999999
telemt_server_connections_total 42
telemt_user_octets_from_client{user="alice"} 100
`
	result := ParseUserMetrics(input)
	require.Len(t, result, 1)
	_, ok := result["alice"]
	require.True(t, ok, "missing user alice")
}

func TestParseUserMetricsSkipsMalformedLines(t *testing.T) {
	input := `telemt_user_octets_from_client{user="alice"} 1000
telemt_user_octets_from_client{user="
telemt_user_octets_from_client 500
telemt_user_octets_from_client{user="bob"} notanumber
telemt_user_octets_to_client{user="alice"} 2000 1616161616000
`
	metrics := ParseUserMetrics(input)
	require.Len(t, metrics, 2)
	assert.Equal(t, uint64(1000), metrics["alice"].OctetsFromClient)
	assert.Equal(t, uint64(2000), metrics["alice"].OctetsToClient)
	// bob has 0 because "notanumber" parses to 0
	assert.Equal(t, uint64(0), metrics["bob"].OctetsFromClient)
}
