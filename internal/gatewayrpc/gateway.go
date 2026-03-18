package gatewayrpc

import (
	"context"
	"encoding/json"

	"google.golang.org/grpc"
	"google.golang.org/grpc/encoding"
)

const (
	// JSONCodecName identifies the custom JSON codec shared by the control-plane and agents.
	JSONCodecName = "json"
	serviceName   = "panvex.AgentGateway"
)

func init() {
	encoding.RegisterCodec(jsonCodec{})
}

// EnrollRequest carries the one-time enrollment token and initial agent metadata.
type EnrollRequest struct {
	Token    string `json:"token"`
	NodeName string `json:"node_name"`
	Version  string `json:"version"`
}

// EnrollResponse returns the issued agent identity and mTLS materials.
type EnrollResponse struct {
	AgentID        string `json:"agent_id"`
	CertificatePEM string `json:"certificate_pem"`
	PrivateKeyPEM  string `json:"private_key_pem"`
	CAPEM          string `json:"ca_pem"`
	ExpiresAtUnix  int64  `json:"expires_at_unix"`
}

// RenewCertificateRequest asks for a new short-lived certificate for an existing agent.
type RenewCertificateRequest struct {
	AgentID string `json:"agent_id"`
}

// RenewCertificateResponse returns the rotated mTLS certificate bundle.
type RenewCertificateResponse struct {
	CertificatePEM string `json:"certificate_pem"`
	PrivateKeyPEM  string `json:"private_key_pem"`
	CAPEM          string `json:"ca_pem"`
	ExpiresAtUnix  int64  `json:"expires_at_unix"`
}

// Heartbeat carries lightweight liveness information from the agent.
type Heartbeat struct {
	AgentID        string `json:"agent_id"`
	NodeName       string `json:"node_name"`
	FleetGroupID   string `json:"fleet_group_id"`
	Version        string `json:"version"`
	ReadOnly       bool   `json:"read_only"`
	ObservedAtUnix int64  `json:"observed_at_unix"`
}

// InstanceSnapshot carries Telemt inventory for one locally managed instance.
type InstanceSnapshot struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	Version           string `json:"version"`
	ConfigFingerprint string `json:"config_fingerprint"`
	ConnectedUsers    int    `json:"connected_users"`
	ReadOnly          bool   `json:"read_only"`
}

// ClientUsageSnapshot carries current client-level usage observed on one agent.
type ClientUsageSnapshot struct {
	ClientID         string `json:"client_id"`
	TrafficUsedBytes uint64 `json:"traffic_used_bytes"`
	UniqueIPsUsed    int    `json:"unique_ips_used"`
	ActiveTCPConns   int    `json:"active_tcp_conns"`
}

// RuntimeDCSnapshot carries one DC health row normalized for the control-plane.
type RuntimeDCSnapshot struct {
	DC                 int     `json:"dc"`
	AvailableEndpoints int     `json:"available_endpoints"`
	AvailablePct       float64 `json:"available_pct"`
	RequiredWriters    int     `json:"required_writers"`
	AliveWriters       int     `json:"alive_writers"`
	CoveragePct        float64 `json:"coverage_pct"`
	RTTMs              float64 `json:"rtt_ms"`
	Load               int     `json:"load"`
}

// RuntimeUpstreamRowSnapshot carries one normalized upstream health row.
type RuntimeUpstreamRowSnapshot struct {
	UpstreamID         int     `json:"upstream_id"`
	RouteKind          string  `json:"route_kind"`
	Address            string  `json:"address"`
	Healthy            bool    `json:"healthy"`
	Fails              int     `json:"fails"`
	EffectiveLatencyMs float64 `json:"effective_latency_ms"`
}

// RuntimeUpstreamSnapshot carries the upstream health summary and detail rows.
type RuntimeUpstreamSnapshot struct {
	ConfiguredTotal int                         `json:"configured_total"`
	HealthyTotal    int                         `json:"healthy_total"`
	UnhealthyTotal  int                         `json:"unhealthy_total"`
	DirectTotal     int                         `json:"direct_total"`
	SOCKS5Total     int                         `json:"socks5_total"`
	Rows            []RuntimeUpstreamRowSnapshot `json:"rows"`
}

// RuntimeEventSnapshot carries one recent Telemt runtime event.
type RuntimeEventSnapshot struct {
	Sequence      uint64 `json:"sequence"`
	TimestampUnix int64  `json:"timestamp_unix"`
	EventType     string `json:"event_type"`
	Context       string `json:"context"`
}

// RuntimeSnapshot carries the normalized Telemt operator overview for one node.
type RuntimeSnapshot struct {
	AcceptingNewConnections bool                  `json:"accepting_new_connections"`
	MERuntimeReady          bool                  `json:"me_runtime_ready"`
	ME2DCFallbackEnabled    bool                  `json:"me2dc_fallback_enabled"`
	UseMiddleProxy          bool                  `json:"use_middle_proxy"`
	StartupStatus           string                `json:"startup_status"`
	StartupStage            string                `json:"startup_stage"`
	StartupProgressPct      float64               `json:"startup_progress_pct"`
	InitializationStatus    string                `json:"initialization_status"`
	Degraded                bool                  `json:"degraded"`
	InitializationStage     string                `json:"initialization_stage"`
	InitializationProgressPct float64             `json:"initialization_progress_pct"`
	TransportMode           string                `json:"transport_mode"`
	CurrentConnections      int                   `json:"current_connections"`
	CurrentConnectionsME    int                   `json:"current_connections_me"`
	CurrentConnectionsDirect int                  `json:"current_connections_direct"`
	ActiveUsers             int                   `json:"active_users"`
	ConnectionsTotal        uint64                `json:"connections_total"`
	ConnectionsBadTotal     uint64                `json:"connections_bad_total"`
	HandshakeTimeoutsTotal  uint64                `json:"handshake_timeouts_total"`
	ConfiguredUsers         int                   `json:"configured_users"`
	DCs                     []RuntimeDCSnapshot   `json:"dcs"`
	Upstreams               RuntimeUpstreamSnapshot `json:"upstreams"`
	RecentEvents            []RuntimeEventSnapshot `json:"recent_events"`
}

// Snapshot carries the current agent inventory and aggregated metrics.
type Snapshot struct {
	AgentID        string                `json:"agent_id"`
	NodeName       string                `json:"node_name"`
	FleetGroupID   string                `json:"fleet_group_id"`
	Version        string                `json:"version"`
	ReadOnly       bool                  `json:"read_only"`
	ObservedAtUnix int64                 `json:"observed_at_unix"`
	Instances      []InstanceSnapshot    `json:"instances"`
	Metrics        map[string]uint64     `json:"metrics"`
	Clients        []ClientUsageSnapshot `json:"clients"`
	Runtime        RuntimeSnapshot       `json:"runtime"`
}

// JobCommand carries an accepted control-plane action toward a specific agent.
type JobCommand struct {
	ID             string   `json:"id"`
	Action         string   `json:"action"`
	IdempotencyKey string   `json:"idempotency_key"`
	TargetAgentIDs []string `json:"target_agent_ids"`
	PayloadJSON    string   `json:"payload_json"`
}

// JobResult carries the execution outcome of one agent-side command.
type JobResult struct {
	AgentID        string `json:"agent_id"`
	JobID          string `json:"job_id"`
	Success        bool   `json:"success"`
	Message        string `json:"message"`
	ObservedAtUnix int64  `json:"observed_at_unix"`
	ResultJSON     string `json:"result_json"`
}

// ConnectClientMessage is sent from an agent to the control-plane stream.
type ConnectClientMessage struct {
	Heartbeat *Heartbeat `json:"heartbeat,omitempty"`
	Snapshot  *Snapshot  `json:"snapshot,omitempty"`
	JobResult *JobResult `json:"job_result,omitempty"`
}

// ConnectServerMessage is sent from the control-plane to an agent stream.
type ConnectServerMessage struct {
	Job *JobCommand `json:"job,omitempty"`
}

// GatewayServer describes the server-side transport contract for agents.
type GatewayServer interface {
	Enroll(context.Context, *EnrollRequest) (*EnrollResponse, error)
	RenewCertificate(context.Context, *RenewCertificateRequest) (*RenewCertificateResponse, error)
	Connect(Gateway_ConnectServer) error
}

// GatewayClient describes the client-side transport contract used by agents.
type GatewayClient interface {
	Enroll(context.Context, *EnrollRequest, ...grpc.CallOption) (*EnrollResponse, error)
	RenewCertificate(context.Context, *RenewCertificateRequest, ...grpc.CallOption) (*RenewCertificateResponse, error)
	Connect(context.Context, ...grpc.CallOption) (Gateway_ConnectClient, error)
}

// Gateway_ConnectServer is the bidirectional stream used by the server implementation.
type Gateway_ConnectServer interface {
	Send(*ConnectServerMessage) error
	Recv() (*ConnectClientMessage, error)
	grpc.ServerStream
}

// Gateway_ConnectClient is the bidirectional stream used by agents.
type Gateway_ConnectClient interface {
	Send(*ConnectClientMessage) error
	Recv() (*ConnectServerMessage, error)
	grpc.ClientStream
}

// NewGatewayClient constructs a client wrapper around a grpc.ClientConnInterface.
func NewGatewayClient(conn grpc.ClientConnInterface) GatewayClient {
	return &gatewayClient{conn: conn}
}

// RegisterGatewayServer registers the agent gateway service on a grpc server.
func RegisterGatewayServer(registrar grpc.ServiceRegistrar, server GatewayServer) {
	registrar.RegisterService(&Gateway_ServiceDesc, server)
}

// Gateway_ServiceDesc describes the manually registered gRPC service.
var Gateway_ServiceDesc = grpc.ServiceDesc{
	ServiceName: serviceName,
	HandlerType: (*GatewayServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "Enroll",
			Handler:    _Gateway_Enroll_Handler,
		},
		{
			MethodName: "RenewCertificate",
			Handler:    _Gateway_RenewCertificate_Handler,
		},
	},
	Streams: []grpc.StreamDesc{
		{
			StreamName:    "Connect",
			Handler:       _Gateway_Connect_Handler,
			ServerStreams: true,
			ClientStreams: true,
		},
	},
}

type gatewayClient struct {
	conn grpc.ClientConnInterface
}

func (c *gatewayClient) Enroll(ctx context.Context, request *EnrollRequest, options ...grpc.CallOption) (*EnrollResponse, error) {
	response := new(EnrollResponse)
	if err := c.conn.Invoke(ctx, "/"+serviceName+"/Enroll", request, response, options...); err != nil {
		return nil, err
	}

	return response, nil
}

func (c *gatewayClient) RenewCertificate(ctx context.Context, request *RenewCertificateRequest, options ...grpc.CallOption) (*RenewCertificateResponse, error) {
	response := new(RenewCertificateResponse)
	if err := c.conn.Invoke(ctx, "/"+serviceName+"/RenewCertificate", request, response, options...); err != nil {
		return nil, err
	}

	return response, nil
}

func (c *gatewayClient) Connect(ctx context.Context, options ...grpc.CallOption) (Gateway_ConnectClient, error) {
	stream, err := c.conn.NewStream(ctx, &Gateway_ServiceDesc.Streams[0], "/"+serviceName+"/Connect", options...)
	if err != nil {
		return nil, err
	}

	return &gatewayConnectClient{ClientStream: stream}, nil
}

type gatewayConnectClient struct {
	grpc.ClientStream
}

func (c *gatewayConnectClient) Send(message *ConnectClientMessage) error {
	return c.ClientStream.SendMsg(message)
}

func (c *gatewayConnectClient) Recv() (*ConnectServerMessage, error) {
	message := new(ConnectServerMessage)
	if err := c.ClientStream.RecvMsg(message); err != nil {
		return nil, err
	}

	return message, nil
}

func _Gateway_Enroll_Handler(server any, ctx context.Context, decoder func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
	request := new(EnrollRequest)
	if err := decoder(request); err != nil {
		return nil, err
	}

	if interceptor == nil {
		return server.(GatewayServer).Enroll(ctx, request)
	}

	info := &grpc.UnaryServerInfo{
		Server:     server,
		FullMethod: "/" + serviceName + "/Enroll",
	}
	handler := func(ctx context.Context, request any) (any, error) {
		return server.(GatewayServer).Enroll(ctx, request.(*EnrollRequest))
	}

	return interceptor(ctx, request, info, handler)
}

func _Gateway_RenewCertificate_Handler(server any, ctx context.Context, decoder func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
	request := new(RenewCertificateRequest)
	if err := decoder(request); err != nil {
		return nil, err
	}

	if interceptor == nil {
		return server.(GatewayServer).RenewCertificate(ctx, request)
	}

	info := &grpc.UnaryServerInfo{
		Server:     server,
		FullMethod: "/" + serviceName + "/RenewCertificate",
	}
	handler := func(ctx context.Context, request any) (any, error) {
		return server.(GatewayServer).RenewCertificate(ctx, request.(*RenewCertificateRequest))
	}

	return interceptor(ctx, request, info, handler)
}

func _Gateway_Connect_Handler(server any, stream grpc.ServerStream) error {
	return server.(GatewayServer).Connect(&gatewayConnectServer{ServerStream: stream})
}

type gatewayConnectServer struct {
	grpc.ServerStream
}

func (s *gatewayConnectServer) Send(message *ConnectServerMessage) error {
	return s.ServerStream.SendMsg(message)
}

func (s *gatewayConnectServer) Recv() (*ConnectClientMessage, error) {
	message := new(ConnectClientMessage)
	if err := s.ServerStream.RecvMsg(message); err != nil {
		return nil, err
	}

	return message, nil
}

type jsonCodec struct{}

func (jsonCodec) Marshal(value any) ([]byte, error) {
	return json.Marshal(value)
}

func (jsonCodec) Unmarshal(data []byte, value any) error {
	return json.Unmarshal(data, value)
}

func (jsonCodec) Name() string {
	return JSONCodecName
}
