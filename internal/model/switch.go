// Package model содержит глобальные модели данных приложения.
// Модели доступны всем воркерам (в том числе из internal/workers) и
// описывают информацию о коммутаторе и его портах, собранную через SNMP.
package model

import (
	"net"
	"time"
)

// Switch — информация о сетевом коммутаторе и его портах,
// собранная через SNMP.
type Switch struct {
	// IP — IP-адрес коммутатора.
	IP string `json:"ip"`

	// MAC — системный MAC-адрес коммутатора.
	MAC string `json:"mac"`

	// Hostname — имя хоста коммутатора (sysName).
	Hostname string `json:"hostname"`

	// Description — описание коммутатора (sysDescr).
	Description string `json:"description"`

	// Location — местоположение коммутатора (sysLocation).
	Location string `json:"location"`

	// Contact — контактное лицо, ответственное за коммутатор (sysContact).
	Contact string `json:"contact"`

	// ObjectID — идентификатор производителя и модели (sysObjectID).
	ObjectID string `json:"object_id"`

	// Vendor — производитель устройства.
	Vendor string `json:"vendor"`

	// Model — модель устройствa
	Model string `json:"model"`

	// SoftwareVersion — версия программного обеспечения
	SoftwareVersion string `json:"software_version"`

	// Uptime — время работы коммутатора с момента последней
	// перезагрузки (sysUpTime).
	Uptime time.Duration `json:"uptime"`

	// CollectedAt — дата и время сбора информации о коммутаторе.
	CollectedAt time.Time `json:"collected_at"`

	// Ports — порты коммутатора.
	Ports []Port `json:"ports"`
}

// Port — информация о порте коммутатора, собранная через SNMP.
type Port struct {
	// Index — индекс порта (ifIndex).
	Index int `json:"index"`

	// Name — имя порта (ifName, например "Gi1/0/1" или "Ethernet0/1").
	Name string `json:"name"`

	// AdminState — административное состояние порта (ifAdminStatus).
	AdminState AdminState `json:"admin_state"`

	// OperState — операционное состояние порта (ifOperStatus).
	OperState OperState `json:"oper_state"`

	// Speed — текущая скорость порта в Мбит/с.
	Speed int64 `json:"speed"`

	// RateIn — средняя загрузка сети для этого интерфейса. Вычисляется по результатам опроса SNMP (ifInOctets).
	InRaten int64 `json:"in_rate"`

	// RateOut — средняя исходящая сеть для данного интерфейса. Вычисляется по результатам опроса SNMP (ifOutOctets).
	OutRate int64 `json:"out_rate"`

	// InErrors — количество ошибок на порту (ifInErrors)
	InErrors uint64 `json:"in_errors"`

	// OutErrors — количество ошибок на порту (ifOutErrors)
	OutErrors uint64 `json:"out_errors"`

	// LastChange — время последнего изменения состояния порта
	// относительно времени работы коммутатора (ifLastChange).
	LastChange time.Duration `json:"last_change"`

	// Macs — MAC-адреса, обнаруженные на порту.
	// Вычисляются по FDB-таблице коммутатора.
	Macs []net.HardwareAddr `json:"macs"`
}

// AdminState — административное состояние порта.
// Значения соответствуют RFC 2863 (ifAdminStatus).
type AdminState int

const (
	// AdminUp — порт административно включён.
	AdminUp AdminState = iota + 1
	// AdminDown — порт административно выключен.
	AdminDown
	// AdminTesting — порт находится в режиме тестирования.
	AdminTesting
)

// String возвращает текстовое представление административного состояния порта.
func (s AdminState) String() string {
	switch s {
	case AdminUp:
		return "up"
	case AdminDown:
		return "down"
	case AdminTesting:
		return "testing"
	}
	return "unknown"
}

// OperState — операционное состояние порта.
// Значения соответствуют RFC 2863 (ifOperStatus).
type OperState int

const (
	// OperUp — порт работает (готов передавать пакеты).
	OperUp OperState = iota + 1
	// OperDown — порт не работает.
	OperDown
	// OperTesting — порт находится в режиме тестирования.
	OperTesting
	// OperUnknown — состояние порта неизвестно.
	OperUnknown
	// OperDormant — порт находится в дежурном режиме: ожидает внешнего
	// события для перехода в работу (например, настроен протокол PPP).
	OperDormant
	// OperNotPresent — компонент, соответствующий порту, физически
	// отсутствует (например, модуль не установлен).
	OperNotPresent
	// OperLowerLayerDown — порт не работает из-за неработающего
	// нижележащего уровня (например, оптоволокно не подключено).
	OperLowerLayerDown
)

// String возвращает текстовое представление операционного состояния порта.
func (s OperState) String() string {
	switch s {
	case OperUp:
		return "up"
	case OperDown:
		return "down"
	case OperTesting:
		return "testing"
	case OperUnknown:
		return "unknown"
	case OperDormant:
		return "dormant"
	case OperNotPresent:
		return "notPresent"
	case OperLowerLayerDown:
		return "lowerLayerDown"
	}
	return "unknown"
}
