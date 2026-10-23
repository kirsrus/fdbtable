// Package model содержит глобальные модели данных приложения.
// Модели доступны всем воркерам (в том числе из internal/workers) и
// описывают информацию о коммутаторе и его портах, собранную через SNMP.
package model

// Device — информация о сетевом устройстве (клиенте сети), адреса которого
// обнаруживаются в FDB-таблицах коммутаторов.
type Device struct {
	// IP — IP-адрес устройства.
	IP string `json:"ip"`

	// Hostname — имя устройства.
	Hostname string `json:"hostname"`

	// MAC — MAC-адрес устройства.
	// Используется как уникальный ключ записи в базе данных.
	MAC string `json:"mac"`

	// Location — местоположение устройства.
	Location string `json:"location"`

	// Description — описание устройства.
	Description string `json:"description"`

	// Type — тип устройства (например, сервер, рабочая станция, принтер).
	Type string `json:"type"`
}
