package server

// EffectiveHTTPListenAddress returns the HTTP bind address the panel should
// listen on: the live store value (env-override > db-seed > registry default
// :8080), or the panelRuntime fallback when no store is wired (test fixtures).
//
// On a store-backed boot, OperationalStore.Reload always populates a value for
// every operational field (env override, stored value, or registry default),
// so RawByName never returns "" for http.listen_address once Reload has run.
// The panelRuntime fallback only matters for the no-store path, which never
// binds a listener.
func (s *Server) EffectiveHTTPListenAddress() string {
	if s.settings != nil {
		if v := s.settings.RawByName("http.listen_address"); v != "" {
			return v
		}
	}
	return s.panelRuntime.HTTPListenAddress
}

// EffectiveGRPCListenAddress mirrors EffectiveHTTPListenAddress for gRPC.
func (s *Server) EffectiveGRPCListenAddress() string {
	if s.settings != nil {
		if v := s.settings.RawByName("grpc.listen_address"); v != "" {
			return v
		}
	}
	return s.panelRuntime.GRPCListenAddress
}
