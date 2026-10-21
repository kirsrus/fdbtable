// Package model содержит глобальные модели данных приложения.
// Модели доступны всем воркерам (в том числе из internal/workers) и
// описывают информацию о коммутаторе и его портах, собранную через SNMP.
package model

import "strings"

// CollectType — тип сбора информации о коммутаторе.
// Хранится в текстовом виде: базовые типы (SNMP, SSH, WEB) или
// персонализированные (например, SNMP_Cisco, SNMP_Eltex_MAS3032,
// WEB_Rujio_808P_v433). Текстовое хранение позволяет добавлять
// новые значения в середину списка без перенумерации.
type CollectType string

// Базовые типы сбора информации о коммутаторе.
// Список будет расширяться новыми типами (в том числе в разных регистрах).
const (
	// CollectTypeSNMP — сбор информации по протоколу SNMP.
	CollectTypeSNMP CollectType = "SNMP"
	// CollectTypeSSH — сбор информации по протоколу SSH.
	CollectTypeSSH CollectType = "SSH"
	// CollectTypeWEB — сбор информации через web-интерфейс коммутатора.
	CollectTypeWEB CollectType = "WEB"
)

// CollectTypes — коллекция допустимых типов сбора информации о коммутаторе.
// Используется для формирования выпадающего списка в шаблоне web-интерфейса
// и для валидации значений, приходящих из формы. Новые типы добавляются
// в этот список, после чего автоматически появляются в интерфейсе.
var CollectTypes = []CollectType{
	CollectTypeSNMP,
	CollectTypeSSH,
	CollectTypeWEB,
}

// String возвращает строковое представление типа сбора.
func (t CollectType) String() string {
	return string(t)
}

// IsValid проверяет, является ли значение допустимым типом сбора.
// Сравнение регистронезависимое: типы могут добавляться в разных регистрах
// (например, "snmp", "Snmp", "SNMP").
func (t CollectType) IsValid() bool {
	for _, ct := range CollectTypes {
		if strings.EqualFold(string(ct), string(t)) {
			return true
		}
	}
	return false
}
