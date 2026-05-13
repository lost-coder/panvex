package enrollment

// ErrorCode is a stable identifier for an enrollment failure mode.
// Codes are part of the public surface area: the UI dispatches on them
// and operators may see them in logs. Never rename an existing code.
type ErrorCode string

const (
	ErrTokenExpired            ErrorCode = "TOKEN_EXPIRED"
	ErrTokenAlreadyUsed        ErrorCode = "TOKEN_ALREADY_USED"
	ErrTokenNotFound           ErrorCode = "TOKEN_NOT_FOUND"
	ErrTLSPinMismatch          ErrorCode = "TLS_PIN_MISMATCH"
	ErrPanelUnreachable        ErrorCode = "PANEL_UNREACHABLE"
	ErrCSRInvalid              ErrorCode = "CSR_INVALID"
	ErrCSRSubjectMismatch      ErrorCode = "CSR_SUBJECT_MISMATCH"
	ErrCertSignFailed          ErrorCode = "CERT_SIGN_FAILED"
	ErrOutboundDialTimeout     ErrorCode = "OUTBOUND_DIAL_TIMEOUT"
	ErrOutboundListenerRefused ErrorCode = "OUTBOUND_LISTENER_REFUSED"
	ErrInternal                ErrorCode = "INTERNAL_ERROR"
)

//nolint:gosec // G101: false positive — values are operator-facing UI strings, not secrets
var messages = map[ErrorCode]string{
	ErrTokenExpired:            "Токен истёк. Сгенерируйте новый в Настройки → Токены подключения.",
	ErrTokenAlreadyUsed:        "Этот токен уже использован. Удалите старого агента или создайте новый токен.",
	ErrTokenNotFound:           "Токен не найден. Проверьте, что команда установки скопирована полностью.",
	ErrTLSPinMismatch:          "Сертификат панели не совпадает с зафиксированным. Если вы намеренно перевыпустили сертификат, обновите pin в конфигурации агента.",
	ErrPanelUnreachable:        "Агент не смог подключиться к панели. Проверьте firewall, DNS и что панель запущена.",
	ErrCSRInvalid:              "Агент прислал некорректный CSR.",
	ErrCSRSubjectMismatch:      "CSR subject не соответствует политике панели.",
	ErrCertSignFailed:          "Панель не смогла подписать сертификат.",
	ErrOutboundDialTimeout:     "Панель не смогла подключиться к слушающему агенту в отведённое время.",
	ErrOutboundListenerRefused: "Агент отказал в подключении. Проверьте, что он запущен и слушает нужный порт.",
	ErrInternal:                "Внутренняя ошибка панели. Подробности в логах.",
}

// MessageFor returns the operator-facing message for code, and ok=false
// if the code is not registered.
func MessageFor(code ErrorCode) (string, bool) {
	msg, ok := messages[code]
	return msg, ok
}
