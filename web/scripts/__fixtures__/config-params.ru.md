# Справочник параметров конфигурации Telemt

В этом документе перечислены все ключи конфигурации, принимаемые `config.toml`.

> [!NOTE]
>
> Этот справочник был составлен с помощью искусственного интеллекта и сверен с базой кода (схема конфигурации, значения по умолчанию и логика проверки).

# Top-level keys


| Ключ | Тип | По умолчанию | Hot-Reload |
| --- | ---- | ------- | ---------- |
| [`dc_overrides`](#dc_overrides) | `Map<String, String or String[]>` | `{}` | `✘` |

## dc_overrides
  - **Описание**: Переопределяет DC эндпоинты для запросов с нестандартными DC; задается в виде строки с индексом DC, значение — один или несколько адресов `ip:port`.

# [general]


| Ключ | Тип | По умолчанию | Hot-Reload |
| --- | ---- | ------- | ---------- |
| [`data_path`](#data_path) | `String` | — | `✘` |
| [`quota_state_path`](#quota_state_path) | `Path` | `"telemt.limit.json"` | `✘` |
| [`config_strict`](#config_strict) | `bool` | `false` | `✘` |
| [`ad_tag`](#ad_tag) | `String` | — | `✔` |
| [`log_level`](#log_level) | `"debug"`, `"verbose"`, `"normal"`, or `"silent"` | `"normal"` | `✔` |
| [`disable_colors`](#disable_colors) | `bool` | `false` | `✘` |

## log_level
  - **Ограничения / валидация**: `"debug"`, `"verbose"`, `"normal"`, или `"silent"`.
  - **Описание**: Уровень детализации логов во время работы системы, который используется только если переменная окружения `RUST_LOG` не задана. Если `RUST_LOG` задана, она имеет приоритет и переопределяет этот параметр.
  - **Пример**:

    ```toml
    [general]
    log_level = "normal"
    ```

# [timeouts]


| Ключ | Тип | По умолчанию | Hot-Reload |
| --- | ---- | ------- | ---------- |
| [`client_first_byte_idle_secs`](#client_first_byte_idle_secs) | `u64` | `300` | `✘` |
| [`client_handshake`](#client_handshake) | `u64` | `30` | `✘` |
| [`client_keepalive`](#client_keepalive) | `u64` | `15` | `✘` |

# [[upstreams]]


| Ключ | Тип | По умолчанию | Hot-Reload |
| --- | ---- | ------- | ---------- |
| [`type`](#type) | `"direct"`, `"socks4"`, `"socks5"`, or `"shadowsocks"` | — | `✘` |
| [`weight`](#weight) | `u16` | `1` | `✘` |
| [`enabled`](#enabled) | `bool` | `true` | `✘` |

# [censorship]


| Ключ | Тип | По умолчанию | Hot-Reload |
| --- | ---- | ------- | ---------- |
| [`tls_domain`](#tls_domain) | `String` | `"petrovich.ru"` | `✘` |
| [`mask`](#mask) | `bool` | `true` | `✘` |
