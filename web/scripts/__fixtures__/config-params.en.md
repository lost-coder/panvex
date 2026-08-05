# Telemt Config Parameters Reference

This document lists all configuration keys accepted by `config.toml`.

> [!NOTE]
>
> This reference was drafted with the help of AI and cross-checked against the codebase (config schema, defaults, and validation logic).

# Top-level keys


| Key | Type | Default | Hot-Reload |
| --- | ---- | ------- | ---------- |
| [`dc_overrides`](#dc_overrides) | `Map<String, String or String[]>` | `{}` | `✘` |

## dc_overrides
  - **Description**: Overrides DC endpoints for non-standard DCs; key is DC index string, value is one or more `ip:port` addresses.

# [general]


| Key | Type | Default | Hot-Reload |
| --- | ---- | ------- | ---------- |
| [`data_path`](#data_path) | `String` | — | `✘` |
| [`quota_state_path`](#quota_state_path) | `Path` | `"telemt.limit.json"` | `✘` |
| [`config_strict`](#config_strict) | `bool` | `false` | `✘` |
| [`ad_tag`](#ad_tag) | `String` | — | `✔` |
| [`log_level`](#log_level) | `"debug"`, `"verbose"`, `"normal"`, or `"silent"` | `"normal"` | `✔` |
| [`disable_colors`](#disable_colors) | `bool` | `false` | `✘` |

## log_level
  - **Constraints / validation**: `"debug"`, `"verbose"`, `"normal"`, or `"silent"`.
  - **Description**: Runtime logging verbosity level (used when `RUST_LOG` is not set). If `RUST_LOG` is set in the environment, it takes precedence over this setting.
  - **Example**:

    ```toml
    [general]
    log_level = "normal"
    ```

# [timeouts]


| Key | Type | Default | Hot-Reload |
| --- | ---- | ------- | ---------- |
| [`client_first_byte_idle_secs`](#client_first_byte_idle_secs) | `u64` | `300` | `✘` |
| [`client_handshake`](#client_handshake) | `u64` | `30` | `✘` |
| [`client_keepalive`](#client_keepalive) | `u64` | `15` | `✘` |

# [[upstreams]]


| Key | Type | Default | Hot-Reload |
| --- | ---- | ------- | ---------- |
| [`type`](#type) | `"direct"`, `"socks4"`, `"socks5"`, or `"shadowsocks"` | — | `✘` |
| [`weight`](#weight) | `u16` | `1` | `✘` |
| [`enabled`](#enabled) | `bool` | `true` | `✘` |

# [censorship]


| Key | Type | Default | Hot-Reload |
| --- | ---- | ------- | ---------- |
| [`tls_domain`](#tls_domain) | `String` | `"petrovich.ru"` | `✘` |
| [`mask`](#mask) | `bool` | `true` | `✘` |
