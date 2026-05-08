package server

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/lost-coder/panvex/internal/controlplane/secretvault"
	"github.com/lost-coder/panvex/internal/controlplane/webhooks"
)

// webhookEndpointDTO is the JSON shape returned by GET endpoints.
// Mirrors webhooks.Endpoint minus the secret (which never leaves the
// server). EventFilter is shipped as a comma-separated string for
// admin-form ergonomics.
type webhookEndpointDTO struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	URL          string `json:"url"`
	EventFilter  string `json:"event_filter"`
	AllowPrivate bool   `json:"allow_private"`
	Enabled      bool   `json:"enabled"`
}

// webhookCreateRequest is the body of POST /api/webhook-endpoints.
// Secret is the plaintext HMAC key the operator wants the receiver
// to verify with — handlers vault-encrypt before persistence.
type webhookCreateRequest struct {
	Name         string `json:"name"`
	URL          string `json:"url"`
	Secret       string `json:"secret"`
	EventFilter  string `json:"event_filter"`
	AllowPrivate bool   `json:"allow_private"`
	Enabled      bool   `json:"enabled"`
}

// webhookUpdateRequest is the body of PUT /api/webhook-endpoints/{id}.
// Empty Secret leaves the existing secret unchanged — opt-in
// rotation, see WebhookStore.UpdateEndpoint godoc.
type webhookUpdateRequest struct {
	Name         string `json:"name"`
	URL          string `json:"url"`
	Secret       string `json:"secret"`
	EventFilter  string `json:"event_filter"`
	AllowPrivate bool   `json:"allow_private"`
	Enabled      bool   `json:"enabled"`
}

func (s *Server) handleListWebhookEndpoints() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.webhookStorage == nil {
			writeErrorWithCode(w, http.StatusServiceUnavailable, "webhook subsystem not configured", "webhooks_disabled")
			return
		}
		eps, err := s.webhookStorage.ListEndpointMeta(r.Context())
		if err != nil {
			s.logger.Error("webhook endpoints list", "error", err)
			writeErrorWithCode(w, http.StatusInternalServerError, "failed to list endpoints", "internal_error")
			return
		}
		out := make([]webhookEndpointDTO, 0, len(eps))
		for _, ep := range eps {
			out = append(out, webhookEndpointDTOFrom(ep))
		}
		writeJSON(w, http.StatusOK, map[string]any{"endpoints": out})
	}
}

func (s *Server) handleGetWebhookEndpoint() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.webhookStorage == nil {
			writeErrorWithCode(w, http.StatusServiceUnavailable, "webhook subsystem not configured", "webhooks_disabled")
			return
		}
		id := chi.URLParam(r, "id")
		ep, err := s.webhookStorage.GetEndpointMeta(r.Context(), id)
		if err != nil {
			if errors.Is(err, webhooks.ErrNotFound) {
				writeErrorWithCode(w, http.StatusNotFound, "webhook endpoint not found", "not_found")
				return
			}
			s.logger.Error("webhook endpoint get", "id", id, "error", err)
			writeErrorWithCode(w, http.StatusInternalServerError, "failed to load endpoint", "internal_error")
			return
		}
		writeJSON(w, http.StatusOK, webhookEndpointDTOFrom(ep))
	}
}

func (s *Server) handleCreateWebhookEndpoint() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.webhookStorage == nil {
			writeErrorWithCode(w, http.StatusServiceUnavailable, "webhook subsystem not configured", "webhooks_disabled")
			return
		}
		var req webhookCreateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErrorWithCode(w, http.StatusBadRequest, "request body must be JSON", "invalid_body")
			return
		}
		if err := validateWebhookForm(req.Name, req.URL, req.Secret, req.EventFilter, true); err != nil {
			writeErrorWithCode(w, http.StatusBadRequest, err.Error(), "invalid_input")
			return
		}
		ciphertext, err := s.encryptWebhookSecret(req.Secret)
		if err != nil {
			s.logger.Error("webhook secret encrypt", "error", err)
			writeErrorWithCode(w, http.StatusInternalServerError, "failed to encrypt secret", "internal_error")
			return
		}
		id := newWebhookEndpointID()
		now := s.now().UTC()
		if err := s.webhookStorage.CreateEndpoint(r.Context(), webhooks.EndpointInput{
			ID:               id,
			Name:             req.Name,
			URL:              req.URL,
			SecretCiphertext: ciphertext,
			EventFilter:      req.EventFilter,
			AllowPrivate:     req.AllowPrivate,
			Enabled:          req.Enabled,
		}, now); err != nil {
			s.logger.Error("webhook endpoint create", "name", req.Name, "error", err)
			writeErrorWithCode(w, http.StatusInternalServerError, "failed to create endpoint", "internal_error")
			return
		}
		actorID := actorIDFromRequest(r)
		_ = s.appendAuditSync(r.Context(), actorID, "webhook.endpoint.create", id, map[string]any{
			"name": req.Name, "url": req.URL,
		})
		writeJSON(w, http.StatusCreated, webhookEndpointDTO{
			ID: id, Name: req.Name, URL: req.URL,
			EventFilter: req.EventFilter, AllowPrivate: req.AllowPrivate, Enabled: req.Enabled,
		})
	}
}

func (s *Server) handleUpdateWebhookEndpoint() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.webhookStorage == nil {
			writeErrorWithCode(w, http.StatusServiceUnavailable, "webhook subsystem not configured", "webhooks_disabled")
			return
		}
		id := chi.URLParam(r, "id")
		var req webhookUpdateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErrorWithCode(w, http.StatusBadRequest, "request body must be JSON", "invalid_body")
			return
		}
		if err := validateWebhookForm(req.Name, req.URL, req.Secret, req.EventFilter, false); err != nil {
			writeErrorWithCode(w, http.StatusBadRequest, err.Error(), "invalid_input")
			return
		}
		var ciphertext string
		if req.Secret != "" {
			ct, err := s.encryptWebhookSecret(req.Secret)
			if err != nil {
				s.logger.Error("webhook secret encrypt", "error", err)
				writeErrorWithCode(w, http.StatusInternalServerError, "failed to encrypt secret", "internal_error")
				return
			}
			ciphertext = ct
		}
		now := s.now().UTC()
		err := s.webhookStorage.UpdateEndpoint(r.Context(), webhooks.EndpointInput{
			ID:               id,
			Name:             req.Name,
			URL:              req.URL,
			SecretCiphertext: ciphertext,
			EventFilter:      req.EventFilter,
			AllowPrivate:     req.AllowPrivate,
			Enabled:          req.Enabled,
		}, now)
		if err != nil {
			if errors.Is(err, webhooks.ErrNotFound) {
				writeErrorWithCode(w, http.StatusNotFound, "webhook endpoint not found", "not_found")
				return
			}
			s.logger.Error("webhook endpoint update", "id", id, "error", err)
			writeErrorWithCode(w, http.StatusInternalServerError, "failed to update endpoint", "internal_error")
			return
		}
		actorID := actorIDFromRequest(r)
		_ = s.appendAuditSync(r.Context(), actorID, "webhook.endpoint.update", id, map[string]any{
			"name": req.Name, "url": req.URL,
			"secret_rotated": req.Secret != "",
		})
		writeJSON(w, http.StatusOK, webhookEndpointDTO{
			ID: id, Name: req.Name, URL: req.URL,
			EventFilter: req.EventFilter, AllowPrivate: req.AllowPrivate, Enabled: req.Enabled,
		})
	}
}

func (s *Server) handleDeleteWebhookEndpoint() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.webhookStorage == nil {
			writeErrorWithCode(w, http.StatusServiceUnavailable, "webhook subsystem not configured", "webhooks_disabled")
			return
		}
		id := chi.URLParam(r, "id")
		if err := s.webhookStorage.DeleteEndpoint(r.Context(), id); err != nil {
			if errors.Is(err, webhooks.ErrNotFound) {
				writeErrorWithCode(w, http.StatusNotFound, "webhook endpoint not found", "not_found")
				return
			}
			s.logger.Error("webhook endpoint delete", "id", id, "error", err)
			writeErrorWithCode(w, http.StatusInternalServerError, "failed to delete endpoint", "internal_error")
			return
		}
		actorID := actorIDFromRequest(r)
		_ = s.appendAuditSync(r.Context(), actorID, "webhook.endpoint.delete", id, nil)
		w.WriteHeader(http.StatusNoContent)
	}
}

func webhookEndpointDTOFrom(ep webhooks.Endpoint) webhookEndpointDTO {
	return webhookEndpointDTO{
		ID:           ep.ID,
		Name:         ep.Name,
		URL:          ep.URL,
		EventFilter:  strings.Join(ep.EventFilter, ","),
		AllowPrivate: ep.AllowPrivate,
		Enabled:      ep.Enabled,
	}
}

// validateWebhookForm is shared by Create + Update. requireSecret
// is true on Create (empty Secret means "no key set", which would
// allow anyone to forge deliveries) and false on Update (empty
// Secret means "leave existing").
func validateWebhookForm(name, urlStr, secret, filter string, requireSecret bool) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("name is required")
	}
	if len(name) > 128 {
		return errors.New("name too long (max 128)")
	}
	if requireSecret && strings.TrimSpace(secret) == "" {
		return errors.New("secret is required")
	}
	if len(secret) > 1024 {
		return errors.New("secret too long (max 1024)")
	}
	parsed, err := url.Parse(strings.TrimSpace(urlStr))
	if err != nil {
		return errors.New("url is invalid")
	}
	switch parsed.Scheme {
	case "https":
		// always ok
	case "http":
		// the worker's runtime preflight enforces
		// PANVEX_ALLOW_INSECURE_WEBHOOK; we accept the value here
		// (admins can store an http:// URL knowing the worker will
		// refuse it without the env var). Keeping the validator
		// strictly schema-shaped means the form doesn't have to
		// dual-read env vars.
	default:
		return errors.New("url must be http(s)")
	}
	if parsed.Host == "" {
		return errors.New("url has no host")
	}
	// event_filter syntax: comma-separated, each entry either
	// "exact.action" or "prefix.*". Reject other characters early.
	for _, raw := range strings.Split(filter, ",") {
		entry := strings.TrimSpace(raw)
		if entry == "" {
			continue
		}
		if !validEventFilterEntry(entry) {
			return errors.New("event_filter entry must be a dot-namespaced action or prefix.*")
		}
	}
	return nil
}

func validEventFilterEntry(entry string) bool {
	candidate := strings.TrimSuffix(entry, ".*")
	if candidate == "" {
		return false
	}
	for _, r := range candidate {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '.' || r == '_' || r == '-':
		default:
			return false
		}
	}
	return true
}

// encryptWebhookSecret runs the operator-supplied plaintext through
// the secret vault under DomainWebhookSecret. nil/zero vaults
// return the plaintext unchanged (matches the rest of the codebase
// for dev installs without an encryption key).
func (s *Server) encryptWebhookSecret(plaintext string) (string, error) {
	if s.secretVault == nil {
		return plaintext, nil
	}
	return s.secretVault.Encrypt(secretvault.DomainWebhookSecret, plaintext)
}

// newWebhookEndpointID returns a 32-hex-char (16 byte) random id.
// Same shape as outbox row IDs so logs/audit have a uniform format.
func newWebhookEndpointID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return "wh-" + hex.EncodeToString(b[:])
}

// actorIDFromRequest extracts the user id from the request auth
// context for audit logging. Returns "system" when the request has
// no session (won't happen on these admin-gated routes — defensive
// fall-through, audit log uses it to distinguish CLI bootstrap
// events from operator actions).
func actorIDFromRequest(r *http.Request) string {
	if _, user, ok := requestAuthContext(r); ok {
		return user.ID
	}
	return "system"
}
