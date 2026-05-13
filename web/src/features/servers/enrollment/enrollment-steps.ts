// Russian-language labels for each enrollment timeline step. Keys
// match enrollment.Step constants in internal/controlplane/enrollment;
// the value renders in the dashboard's EnrollmentTimeline when the
// backend publishes a matching `enrollment.event`.
//
// Unknown step keys fall back to the raw machine token at render
// time, so this map can be extended after a backend addition without
// blocking the UI.
export const STEP_LABELS_RU: Record<string, string> = {
  bootstrap_request_received: "Запрос на установку получен панелью",
  token_validated: "Токен проверен",
  csr_received: "CSR получен",
  csr_validated: "CSR проверен",
  cert_signed: "Сертификат подписан",
  cert_returned: "Сертификат отправлен агенту",
  agent_persisted_cert: "Агент сохранил сертификат",
  gateway_dialed: "Агент подключается к gRPC-шлюзу",
  tls_handshake_ok: "TLS-рукопожатие завершено",
  first_sync_ok: "Первая синхронизация успешна",
  install_command_issued: "Команда установки выдана",
  outbound_listener_started: "Агент слушает входящие подключения",
  panel_dial_attempted: "Панель подключается к агенту",
};
