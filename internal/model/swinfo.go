// Package model содержит глобальные модели данных приложения.
// Модели доступны всем воркерам (в том числе из internal/workers) и
// описывают информацию о коммутаторе и его портах, собранную через SNMP.
package model

// SwInfo — информация о сетевом коммутаторе.
type SwInfo struct {
	IP          string `json:"ip"`
	MAC         string `json:"mac"`
	Hostname    string `json:"hostname"`
	Location    string `json:"location"`
	Description string `json:"description"`
	// CollectType — тип сбора информации о коммутаторе.
	// Хранится в текстовом виде: базовые типы (SNMP, SSH, WEB) или
	// персонализированные (например, SNMP_Cisco, SNMP_Eltex_MAS3032,
	// WEB_Rujio_808P_v433). Текстовое хранение позволяет добавлять
	// новые значения в середину списка без перенумерации.
	CollectType string `json:"collect_type"`
	Username    string `json:"username"`
	Password    string `json:"password"`
}
