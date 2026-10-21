// Этот файл содержит единый интерфейс сбора информации с сетевых
// коммутаторов, реестр реализаций и фабрику выбора реализации по типу сбора.
package collector

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/kirsrus/fdbtable/internal/model"
)

// DefaultCollectTimeout — таймаут сбора информации с одного коммутатора
// по умолчанию (5 минут). Таймаут завершает зависший сбор данных,
// например при отказе оборудования, а отмена родительского контекста
// позволяет остановить сбор при завершении работы воркера.
const DefaultCollectTimeout = 5 * time.Minute

// ErrUnknownCollectType — ошибка выбора реализации сбора данных:
// тип сбора не зарегистрирован и не соответствует ни одной базовой
// реализации.
var ErrUnknownCollectType = errors.New("неизвестный тип сбора данных")

// Collector — единый интерфейс сбора информации с сетевого коммутатора.
// Реализации получают контекст (для отмены и таймаута) и полную
// информацию о коммутаторе из очереди заданий, а возвращают собранную
// модель коммутатора.
//
// Базовые реализации: SNMP, SSH, WEB. Специализированные типы сбора
// (например, SNMP_Cisco, SSH_AllietTelesis_908, WEB_Rujio_808P_v433)
// выбираются внутри реӓлизаций, а пока представлены заглушками базовых
// типов.
type Collector interface {
	// Name возвращает имя типа сбора данных (например, "SNMP", "SSH",
	// "WEB" или зарегистрированный специализированный тип).
	Name() string

	// Collect выполняет сбор информации с коммутатора.
	// ctx позволяет отменить сбор (отмена родительского контекста) или
	// завершить его по таймауту (по умолчанию DefaultCollectTimeout).
	// sw содержит всю информацию о коммутаторе из очереди заданий.
	Collect(ctx context.Context, sw model.SwInfo) (model.Switch, error)
}

// registry — реестр реализаций сбора информации, ключом служит
// нормализованное имя типа сбора.
type registry struct {
	mu     sync.RWMutex
	byName map[string]Collector
}

// collectorRegistry — глобальный реестр реализаций пакета.
// Базовые реализации регистрируются в инициализаторах своих файлов,
// специализированные могут добавляться позже через RegisterCollector.
var collectorRegistry = newRegistry()

// newRegistry создаёт пустой реестр реализаций.
func newRegistry() *registry {
	return &registry{byName: make(map[string]Collector)}
}

// RegisterCollector регистрирует реализацию сбора по имени типа.
// Имя нормализуется к верхнему регистру, поэтому специализированная
// реализация (например, SNMP_Cisco) перекрывает базовую (SNMP) при
// запросе по точному совпадению имени.
func RegisterCollector(name string, c Collector) {
	collectorRegistry.mu.Lock()
	defer collectorRegistry.mu.Unlock()
	collectorRegistry.byName[normalizeType(name)] = c
}

// NewCollector возвращает реализацию сбора информации для заданного типа.
// Сначала ищется точное совпадение имени (в том числе специализированная
// реализация), затем — базовая реализация по префиксу типа:
//
//	SNMP_Cisco              → SNMP (заглушка)
//	SSH_AllietTelesis_908   → SSH (заглушка)
//	WEB_Rujio_808P_v433     → WEB (заглушка)
func NewCollector(collectType string) (Collector, error) {
	return collectorRegistry.collector(collectType)
}

// collector ищет реализацию в реестре по имени типа сбора.
func (r *registry) collector(collectType string) (Collector, error) {
	name := normalizeType(collectType)
	if name == "" {
		return nil, ErrUnknownCollectType
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	// Точное совпадение: базовая (SNMP, SSH, WEB) или зарегистрированная
	// специализированная реализация.
	if c, ok := r.byName[name]; ok {
		return c, nil
	}

	// Специализированная реализация не зарегистрирована — используем
	// базовую по префиксу имени: SNMP_Cisco → SNMP, SSH_* → SSH и т.п.
	if c, ok := r.byName[baseType(name)]; ok {
		return c, nil
	}

	return nil, ErrUnknownCollectType
}

// normalizeType приводит имя типа сбора к нормализованному виду (верхний
// регистр без обрамляющих пробелов), чтобы значения "snmp_cisco" и
// "SNMP_Cisco" соответствовали одной записи реестра.
func normalizeType(name string) string {
	return strings.ToUpper(strings.TrimSpace(name))
}

// baseType возвращает базовый тип сбора по имени: часть имени до первого
// символа подчёркивания. Для "SSH_AllietTelesis_908" вернёт "SSH",
// для "SNMP" — "SNMP".
func baseType(name string) string {
	if i := strings.IndexByte(name, '_'); i > 0 {
		return name[:i]
	}
	return name
}
