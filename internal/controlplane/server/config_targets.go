package server

import (
	"fmt"
	"strings"
)

// editableConfigSections is the allowlist of top-level Telemt config
// sections an operator may store via the config-target endpoints. It
// mirrors Telemt's PATCH /v1/config EDITABLE_SECTIONS exactly — any other
// top-level key (e.g. access/server/network) is rejected with 400 on PUT.
// Note show_link is NOT editable: it is a legacy key Telemt auto-migrates
// to general.links.show on load and rejects on PATCH (telemt-audit F4).
var editableConfigSections = map[string]struct{}{
	"general": {}, "timeouts": {}, "censorship": {},
	"upstreams": {}, "dc_overrides": {},
}

// validateEditableSections returns an error if any top-level key is not
// in the editable allowlist.
func validateEditableSections(sections map[string]any) error {
	for k := range sections {
		if _, ok := editableConfigSections[k]; !ok {
			return fmt.Errorf("section not editable: %s", k)
		}
	}
	return nil
}

// processOwnedPaths mirrors web/src/features/servers/config/guard.ts
// PROCESS_OWNED_PATHS: fields Telemt reads only at process start. The
// in-process Maestro reload cannot apply changes to them — only a real
// process restart can, and that machinery was removed. The web editor
// already keeps these out of CONFIG_FIELDS, but a config target can also
// be written directly against the API, so the server enforces the same
// invariant rather than reporting a false success.
var processOwnedPaths = map[string]struct{}{
	"general.data_path":        {},
	"general.quota_state_path": {},
	"general.disable_colors":   {},
}

// validateNoProcessOwnedFields returns an error if sections contains any of
// the process-owned dotted paths in processOwnedPaths.
func validateNoProcessOwnedFields(sections map[string]any) error {
	for path := range processOwnedPaths {
		section, field, _ := strings.Cut(path, ".")
		sub, ok := sections[section].(map[string]any)
		if !ok {
			continue
		}
		if _, present := sub[field]; present {
			return fmt.Errorf("field %s requires a process restart, which is not supported by in-process reload", path)
		}
	}
	return nil
}

// mapContainerPaths — узлы, чьи дочерние ключи задаёт ОПЕРАТОР, а не схема
// Telemt: dc_overrides ("203") и censorship.exclusive_mask ("hv24s.metrion.icu").
// Точка в таком ключе легитимна, поэтому обход в них не спускается.
//
// Это НАМЕРЕННО более узкий список, чем CONTAINER_PATHS в
// web/src/features/servers/config/paramCatalog.ts: там есть ещё upstreams, здесь
// его быть не должно. upstreams — МАССИВ таблиц, ключи внутри его элементов
// задаёт схема Telemt, поэтому плоский точечный ключ там так же невалиден, как
// в обычной секции, и элементы массива обходятся наравне с остальными. Добавить
// сюда upstreams значило бы навсегда ослепить гард на этой ветке.
var mapContainerPaths = map[string]struct{}{
	"dc_overrides":              {},
	"censorship.exclusive_mask": {},
}

// validateNoDottedKeys отвергает ключ с точкой внутри секции, вложенность
// которой задаёт схема Telemt (например {"general": {"links.public_host": …}}
// вместо {"general": {"links": {"public_host": …}}}).
//
// Такой ключ рождался, когда каталог параметров кодировал вложенность в поле
// key, а редактор трактовал его как один уровень. В конфиг-структурах Telemt
// нет deny_unknown_fields, поэтому serde молча выбрасывал ключ: PATCH отвечал
// 200, на ноде не менялось ничего, а applyDiff находил путь отсутствующим в
// observed на каждом проходе — дрейф не сходился никогда. Фронт починен, но
// проверка живёт и здесь: config target можно записать и мимо UI.
func validateNoDottedKeys(sections map[string]any) error {
	var walk func(prefix string, node map[string]any) error
	var walkValue func(path string, value any) error

	// Спускаться надо и в массивы: upstreams — массив таблиц, и плоский ключ
	// внутри его элемента Telemt проглотит ровно так же молча, как в обычной
	// секции. Элемент массива делит путь с самим массивом — отдельного имени у
	// него нет, а для сообщения об ошибке важна секция, а не индекс.
	walkValue = func(path string, value any) error {
		switch typed := value.(type) {
		case map[string]any:
			return walk(path, typed)
		case []any:
			for _, item := range typed {
				if err := walkValue(path, item); err != nil {
					return err
				}
			}
		}
		return nil
	}

	walk = func(prefix string, node map[string]any) error {
		for k, v := range node {
			path := k
			if prefix != "" {
				path = prefix + "." + k
			}
			if _, isContainer := mapContainerPaths[path]; isContainer {
				continue
			}
			if strings.Contains(k, ".") {
				where := prefix
				if where == "" {
					where = "config root"
				}
				return fmt.Errorf("invalid config key %q in %q: nested tables must be nested, not dotted", k, where)
			}
			if err := walkValue(path, v); err != nil {
				return err
			}
		}
		return nil
	}
	return walk("", sections)
}

// resolveEffectiveConfig merges a group config target with an agent override.
// The agent override wins per field (deep-merged: nested section tables merge
// key-by-key; scalars and arrays in the override replace the group value).
// Either argument may be nil. The result is a fresh map (inputs are not mutated).
func resolveEffectiveConfig(group, override map[string]any) map[string]any {
	out := deepCopyConfigMap(group)
	if out == nil {
		out = map[string]any{}
	}
	deepMergeConfigInto(out, override)
	return out
}

func deepCopyConfigMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		if sub, ok := v.(map[string]any); ok {
			out[k] = deepCopyConfigMap(sub)
		} else {
			out[k] = v
		}
	}
	return out
}

// deepMergeConfigInto overlays patch onto base: matching sub-maps merge
// recursively; every other value (scalars, arrays) replaces.
func deepMergeConfigInto(base, patch map[string]any) {
	for k, pv := range patch {
		pvMap, pIsMap := pv.(map[string]any)
		bvMap, bIsMap := base[k].(map[string]any)
		if pIsMap && bIsMap {
			deepMergeConfigInto(bvMap, pvMap)
		} else if pIsMap {
			base[k] = deepCopyConfigMap(pvMap)
		} else {
			base[k] = pv
		}
	}
}
