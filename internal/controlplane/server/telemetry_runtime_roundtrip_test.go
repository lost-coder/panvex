package server

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

// fillNonZero рекурсивно заполняет каждое settable-поле value не-нулевым
// значением. Новое exported-поле в AgentRuntime автоматически попадает под
// тест: если оно не переживает persist→restore, assertNoZeroFields упадёт.
// Неизвестный kind — это сигнал расширить хелпер, а не молча пропустить поле.
func fillNonZero(t *testing.T, v reflect.Value, path string) {
	t.Helper()
	switch v.Kind() {
	case reflect.Bool:
		v.SetBool(true)
	case reflect.String:
		v.SetString("x-" + path)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v.SetInt(7)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		v.SetUint(9)
	case reflect.Float32, reflect.Float64:
		v.SetFloat(1.5)
	case reflect.Pointer:
		v.Set(reflect.New(v.Type().Elem()))
		fillNonZero(t, v.Elem(), path)
	case reflect.Slice:
		elem := reflect.New(v.Type().Elem()).Elem()
		fillNonZero(t, elem, path+"[0]")
		v.Set(reflect.Append(reflect.MakeSlice(v.Type(), 0, 1), elem))
	case reflect.Struct:
		if v.Type() == reflect.TypeOf(time.Time{}) {
			// UTC без монотонной компоненты: RFC3339Nano сохраняет наносекунды.
			v.Set(reflect.ValueOf(time.Date(2026, time.July, 2, 3, 4, 5, 123456789, time.UTC)))
			return
		}
		for i := 0; i < v.NumField(); i++ {
			field := v.Field(i)
			if !field.CanSet() { // unexported
				continue
			}
			fillNonZero(t, field, path+"."+v.Type().Field(i).Name)
		}
	default:
		t.Fatalf("fillNonZero: неподдерживаемый kind %s в %s — расширь хелпер", v.Kind(), path)
	}
}

// assertNoZeroFields — зеркальный обход: каждое exported-поле восстановленного
// значения обязано остаться не-нулевым.
func assertNoZeroFields(t *testing.T, v reflect.Value, path string) {
	t.Helper()
	switch v.Kind() {
	case reflect.Pointer:
		if v.IsNil() {
			t.Errorf("%s: nil pointer после round-trip", path)
			return
		}
		assertNoZeroFields(t, v.Elem(), path)
	case reflect.Slice:
		if v.Len() == 0 {
			t.Errorf("%s: пустой slice после round-trip", path)
			return
		}
		assertNoZeroFields(t, v.Index(0), path+"[0]")
	case reflect.Struct:
		if v.Type() == reflect.TypeOf(time.Time{}) {
			if v.Interface().(time.Time).IsZero() {
				t.Errorf("%s: zero time.Time после round-trip", path)
			}
			return
		}
		for i := 0; i < v.NumField(); i++ {
			if v.Type().Field(i).PkgPath != "" { // unexported
				continue
			}
			assertNoZeroFields(t, v.Field(i), path+"."+v.Type().Field(i).Name)
		}
	default:
		if v.IsZero() {
			t.Errorf("%s: zero-значение после round-trip (поле потеряно при persist/restore)", path)
		}
	}
}

// TestAgentRuntimeRecordRoundTripLosesNoFields — страховка от дрейфа
// (аудит #3): ВСЕ exported-поля AgentRuntime, заполненные не-нулевыми
// значениями, обязаны пережить persist→restore через
// TelemetryRuntimeCurrentRecord. Раньше терялись 22 поля из 50.
func TestAgentRuntimeRecordRoundTripLosesNoFields(t *testing.T) {
	var original AgentRuntime
	fillNonZero(t, reflect.ValueOf(&original).Elem(), "AgentRuntime")

	record := runtimeCurrentRecordFromAgent(Agent{ID: "agent-rt", Runtime: original})
	if record.AgentID != "agent-rt" {
		t.Fatalf("record.AgentID = %q", record.AgentID)
	}
	if !record.ObservedAt.Equal(original.UpdatedAt) {
		t.Fatalf("record.ObservedAt = %v, want Runtime.UpdatedAt %v", record.ObservedAt, original.UpdatedAt)
	}

	restored := runtimeFromCurrentRecord(record)

	// 1) Ни одно exported-поле не обнулилось.
	assertNoZeroFields(t, reflect.ValueOf(restored), "AgentRuntime")

	// 2) Значения идентичны. Сравниваем через канонический JSON:
	//    reflect.DeepEqual на time.Time чувствителен к внутреннему
	//    представлению (wall/ext) и дал бы ложные срабатывания.
	wantJSON, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal original: %v", err)
	}
	gotJSON, err := json.Marshal(restored)
	if err != nil {
		t.Fatalf("marshal restored: %v", err)
	}
	if string(wantJSON) != string(gotJSON) {
		t.Fatalf("round-trip mismatch:\n got: %s\nwant: %s", gotJSON, wantJSON)
	}
}
