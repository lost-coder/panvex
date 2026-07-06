// Package api holds the control-plane's presentation (view) types — the JSON
// shapes served to the HTTP/WebSocket layer and mirrored by the agents
// LiveStore. They carry no persistence or transport concerns: the package
// imports neither storage nor gatewayrpc, so domain packages (notably
// controlplane/agents) can depend on it without pulling in the server package.
// server keeps type aliases over these so its existing call sites are
// unchanged; the storage <-> view converters live in the server package
// because they touch storage.*Record.
package api
