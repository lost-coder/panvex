package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"

	"github.com/lost-coder/panvex/internal/agent/creds"
	agentstate "github.com/lost-coder/panvex/internal/agent/state"
)

// runBootstrapCommand parses the `agent bootstrap` flags and hands the parsed
// options to internal/agent/creds, which owns the enrollment protocols (HTTPS
// CSR exchange for dial mode, the EnrollOutbound gRPC handshake for reverse
// mode). Everything below is flag plumbing plus the two pre-flight guards that
// only make sense for a CLI invocation (-state-file present, -force required to
// overwrite an existing bundle).
func runBootstrapCommand(args []string, client *http.Client) error {
	flags := flag.NewFlagSet("bootstrap", flag.ContinueOnError)
	panelURL := flags.String("panel-url", "", "Control-plane HTTPS base URL")
	enrollmentToken := flags.String("enrollment-token", "", "One-time enrollment token")
	stateFile := flags.String("state-file", "data/agent-state.json", "Agent credential state file")
	nodeName := flags.String("node-name", hostName(), "Node name reported to the control-plane")
	version := flags.String("version", "dev", "Agent version")
	force := flags.Bool("force", false, "Overwrite an existing state file")
	insecureTransport := flags.Bool("insecure-transport", false,
		"Allow http:// panel URLs on non-loopback hosts. Use only on trusted private networks (e.g. VPN-only links) — bootstrap certificate transits unencrypted when this is set.")
	mode := flags.String("mode", "dial", "bootstrap mode: dial | reverse")
	bootstrapToken := flags.String("bootstrap-token", "", "raw bootstrap token (reverse mode)")
	agentID := flags.String("agent-id", "", "agent identifier (reverse mode)")
	listenAddr := flags.String("listen-addr", ":8443", "TCP listen address (reverse mode)")
	caPin := flags.String("ca-pin", "", "SHA-256 SPKI hash of panel CA, base64url (reverse mode)")
	panelCN := flags.String("panel-cn", "", "expected CN/SAN of panel client cert (reverse mode)")
	reversePanelURL := flags.String("panel-url-grpc", "", "gRPC endpoint of the panel, host:port (reverse mode, e.g. panel.example.com:8443)")
	if err := flags.Parse(args); err != nil {
		return err
	}

	if *stateFile == "" {
		return errors.New("bootstrap requires -state-file")
	}

	if *mode == "reverse" {
		if *bootstrapToken == "" || *agentID == "" || *caPin == "" || *panelCN == "" {
			return errors.New("reverse bootstrap requires --bootstrap-token, --agent-id, --ca-pin, --panel-cn")
		}
		if *reversePanelURL == "" {
			return errors.New("reverse bootstrap requires --panel-url-grpc (gRPC endpoint, e.g. panel.example.com:8443)")
		}
		return creds.ReverseBootstrap(creds.ReverseBootstrapConfig{
			StateFile:      *stateFile,
			BootstrapToken: *bootstrapToken,
			AgentID:        *agentID,
			ListenAddr:     *listenAddr,
			CAPin:          *caPin,
			PanelCN:        *panelCN,
			PanelURL:       *reversePanelURL,
		})
	}

	if *panelURL == "" || *enrollmentToken == "" {
		return errors.New("bootstrap requires -panel-url, -enrollment-token, and -state-file")
	}

	if !*force {
		if _, err := os.Stat(*stateFile); err == nil {
			return errors.New("bootstrap requires -force when the state file already exists")
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}

	if *insecureTransport {
		// Loud warning on every bootstrap. Operators who flipped the flag
		// knowingly will ignore it; anyone who flipped it by accident will
		// see the drift in their install logs and back it out.
		slog.Warn("bootstrap over insecure transport",
			slog.String("panel_url", *panelURL),
			slog.String("hint", "certificate transits unencrypted; only use on VPN / private-network links"))
	}

	credentialsState, err := creds.Bootstrap(context.Background(), client, creds.BootstrapConfig{
		PanelURL:          *panelURL,
		EnrollmentToken:   *enrollmentToken,
		StateFile:         *stateFile,
		NodeName:          *nodeName,
		Version:           *version,
		Force:             *force,
		InsecureTransport: *insecureTransport,
	})
	if err != nil {
		return err
	}

	return agentstate.Save(*stateFile, credentialsState)
}
