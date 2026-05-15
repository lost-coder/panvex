package telemt

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const maxResponseBodySize = 10 << 20 // 10 MiB

func (c *Client) getJSON(ctx context.Context, path string, dest any) error {
	_, err := c.getJSONWithStatus(ctx, path, dest)
	return err
}

// getJSONWithStatus is identical to getJSON but also returns the HTTP
// status code. Callers that want to distinguish a 404 (e.g. an endpoint
// added in a newer Telemt release) from a hard transport / 5xx failure
// use this rather than parsing the wrapped error string.
//
// On a transport-level error or a non-2xx response the status is still
// surfaced (0 when no HTTP exchange completed) so callers can route on
// it; the returned error is the same wrapped Telemt API error getJSON
// would have produced.
func (c *Client) getJSONWithStatus(ctx context.Context, path string, dest any) (int, error) {
	request, err := c.newRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return 0, err
	}

	response, err := c.httpClient.Do(request)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()

	if response.StatusCode >= http.StatusBadRequest {
		return response.StatusCode, fmt.Errorf("telemt request failed: %w", decodeAPIError(response.Body, fmt.Sprintf("telemt request failed with status %d", response.StatusCode)))
	}

	return response.StatusCode, decodeSuccessData(response.Body, dest)
}

func (c *Client) newRequest(ctx context.Context, method string, path string, body any) (*http.Request, error) {
	endpoint := *c.baseURL
	endpoint.Path = path

	var requestBody *bytes.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		requestBody = bytes.NewReader(payload)
	} else {
		requestBody = bytes.NewReader(nil)
	}

	request, err := http.NewRequestWithContext(ctx, method, endpoint.String(), requestBody)
	if err != nil {
		return nil, err
	}

	request.Header.Set("Authorization", c.authorization)
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}

	return request, nil
}

func decodeSuccessData(body io.Reader, dest any) error {
	payload, err := io.ReadAll(io.LimitReader(body, maxResponseBodySize))
	if err != nil {
		return err
	}

	var envelope struct {
		OK   bool            `json:"ok"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(payload, &envelope); err == nil && len(envelope.Data) > 0 {
		return json.Unmarshal(envelope.Data, dest)
	}

	return json.Unmarshal(payload, dest)
}

// formatHTTPErr renders a "<prefix>: <detail>" error string. Centralised so the
// "%s: %s" format literal does not appear at every call site in decodeAPIError
// (Sonar S1192).
func formatHTTPErr(prefix, detail string) error {
	return fmt.Errorf("%s: %s", prefix, detail)
}

func decodeAPIError(body io.Reader, fallback string) error {
	payload, err := io.ReadAll(io.LimitReader(body, maxResponseBodySize))
	if err != nil {
		return err
	}

	var envelope struct {
		OK      bool            `json:"ok"`
		Error   json.RawMessage `json:"error"`
		Message string          `json:"message"`
	}
	if err := json.Unmarshal(payload, &envelope); err == nil {
		code, message := decodeAPIErrorDetails(envelope.Error)
		if message == "" {
			message = strings.TrimSpace(envelope.Message)
		}

		switch {
		case code != "" && message != "":
			return formatHTTPErr(code, message)
		case code != "":
			return errors.New(code)
		case message != "":
			return formatHTTPErr(fallback, message)
		}
	}

	trimmed := strings.Join(strings.Fields(string(payload)), " ")
	if trimmed != "" {
		return formatHTTPErr(fallback, trimmed)
	}

	return errors.New(fallback)
}

func decodeAPIErrorDetails(raw json.RawMessage) (string, string) {
	if len(raw) == 0 {
		return "", ""
	}

	var code string
	if err := json.Unmarshal(raw, &code); err == nil {
		return strings.TrimSpace(code), ""
	}

	var details struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(raw, &details); err == nil {
		return strings.TrimSpace(details.Code), strings.TrimSpace(details.Message)
	}

	return "", ""
}

// parseScopes normalizes the Telemt scopes field which may be a single string or an array of strings.
func parseScopes(v any) []string {
	switch val := v.(type) {
	case string:
		if val == "" {
			return nil
		}
		return []string{val}
	case []any:
		result := make([]string, 0, len(val))
		for _, item := range val {
			if s, ok := item.(string); ok {
				result = append(result, s)
			}
		}
		return result
	default:
		return nil
	}
}

func marshalJSON(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return ""
	}

	return string(data)
}

func marshalRawJSON(value map[string]any) string {
	return marshalJSON(value)
}

func jsonString(value any) string {
	text, ok := value.(string)
	if !ok {
		return ""
	}
	return text
}

func jsonFloat(value any) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case float32:
		return float64(typed)
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	case uint64:
		return float64(typed)
	default:
		return 0
	}
}

// collectConnectionLinks flattens every non-empty link Telemt returned
// into a single ordered slice. Telemt's tls_domains config emits one
// TLS link per domain (×host); we keep each entry distinct so the
// panel can render them all. Order: TLS → Secure → Classic so the
// strongest mode is first.
func collectConnectionLinks(tlsLinks, secureLinks, classicLinks []string) []string {
	out := make([]string, 0, len(tlsLinks)+len(secureLinks)+len(classicLinks))
	for _, group := range [][]string{tlsLinks, secureLinks, classicLinks} {
		for _, link := range group {
			trimmed := strings.TrimSpace(link)
			if trimmed != "" {
				out = append(out, trimmed)
			}
		}
	}
	return out
}

