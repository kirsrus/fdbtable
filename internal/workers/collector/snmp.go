// Этот файл содержит реализацию сбора информации с коммутатора
// через протокол SNMP (заглушка).
package collector

import (
	"context"

	"github.com/kirsrus/fdbtable/internal/model"
)

func init() {
	RegisterCollector(TypeSNMP, NewSNMPCollector())
}

// SNMPCollector — реализация сбора информации с коммутатора по протоколу
// SNMP (заглушка).
//
// Внутри этой реализации будет развиваться поддержка конкретных
// производителей и моделей по типу сбора из очереди заданий (sw.CollectType):
// SNMP_Cisco, SNMP_Eltex_MAS3032 и т.п. Пока все специализированные типы
// обрабатываются общей заглушкой базового типа SNMP.
type SNMPCollector struct{}

// NewSNMPCollector создаёт SNMP-реализацию сбора информации.
func NewSNMPCollector() *SNMPCollector {
	return &SNMPCollector{}
}

// Name возвращает имя типа сбора данных.
func (c *SNMPCollector) Name() string {
	return TypeSNMP
}

// Collect выполняет сбор информации с коммутатора по SNMP (заглушка).
// Принимает контекст для отмены и таймаута сбора, полную информацию
// о коммутаторе из очереди заданий и возвращает собранную модель.
func (c *SNMPCollector) Collect(ctx context.Context, sw model.SwInfo) (model.Switch, error) {
	// TODO: реализовать опрос коммутатора по SNMP. Для специализированных
	// типов (например, SNMP_Cisco) выполнять опрос по профилю производителя.
	// Пока — общая заглушка базового типа.
	if err := waitStub(ctx); err != nil {
		return model.Switch{}, err
	}
	return stubSwitch(sw), nil
}
