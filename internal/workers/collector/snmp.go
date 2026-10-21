// Этот файл содержит реализацию сбора информации с коммутатора
// через протокол SNMP версии 2c: параметры подключения, OID'ы
// стандартных MIB (MIB-II, IF-MIB, BRIDGE-MIB) и реализацию Collector.
package collector

import (
	"context"
	"fmt"
	"time"

	"github.com/gosnmp/gosnmp"
	"github.com/kirsrus/fdbtable/internal/model"
)

// Параметры подключения к SNMP-агенту коммутатора.
const (
	// snmpPort — стандартный порт SNMP-агента (udp/161).
	snmpPort = 161

	// snmpCommunity — комьюнити SNMP v2c.
	snmpCommunity = "public"

	// snmpTimeout — таймаут одного SNMP-запроса.
	snmpTimeout = 5 * time.Second

	// snmpRetries — количество повторов запроса при отсутствии ответа.
	snmpRetries = 1

	// snmpRateSampleInterval — пауза между двумя замерами счётчиков октетов
	// интерфейсов для вычисления текущей скорости передачи (InRaten/OutRate).
	snmpRateSampleInterval = 5 * time.Second
)

// OID'ы системной группы MIB-II (RFC 1213) — соответствуют полям модели
// Switch: описание, объект, время работы, контакт, имя и местоположение.
const (
	oidSysDescr    = "1.3.6.1.2.1.1.1.0"
	oidSysObjectID = "1.3.6.1.2.1.1.2.0"
	oidSysUpTime   = "1.3.6.1.2.1.1.3.0"
	oidSysContact  = "1.3.6.1.2.1.1.4.0"
	oidSysName     = "1.3.6.1.2.1.1.5.0"
	oidSysLocation = "1.3.6.1.2.1.1.6.0"
)

// OID'ы таблицы интерфейсов ifTable (RFC 2863) и расширенной ifXTable —
// соответствуют полям модели Port: индекс, имя, состояние, скорость,
// счётчики октетов и ошибок, время последнего изменения.
const (
	oidIfIndex       = "1.3.6.1.2.1.2.2.1.1"
	oidIfDescr       = "1.3.6.1.2.1.2.2.1.2"
	oidIfSpeed       = "1.3.6.1.2.1.2.2.1.5"
	oidIfPhysAddress = "1.3.6.1.2.1.2.2.1.6"
	oidIfAdminStatus = "1.3.6.1.2.1.2.2.1.7"
	oidIfOperStatus  = "1.3.6.1.2.1.2.2.1.8"
	oidIfLastChange  = "1.3.6.1.2.1.2.2.1.9"
	oidIfInOctets    = "1.3.6.1.2.1.2.2.1.10"
	oidIfInErrors    = "1.3.6.1.2.1.2.2.1.14"
	oidIfOutOctets   = "1.3.6.1.2.1.2.2.1.16"
	oidIfOutErrors   = "1.3.6.1.2.1.2.2.1.20"

	// oidIfName — человекочитаемое имя интерфейса (ifXTable, RFC 2863),
	// например "Gi1/0/1" или "Ethernet0/1". Не все устройства поддерживают.
	oidIfName = "1.3.6.1.2.1.31.1.1.1.1"

	// oidIfHighSpeed — текущая скорость интерфейса в Мбит/с (ifXTable).
	// Используется как дополнение к ifSpeed при больших скоростях.
	oidIfHighSpeed = "1.3.6.1.2.1.31.1.1.1.15"
)

// OID'ы BRIDGE-MIB (RFC 1493) — системный MAC-адрес моста и таблица
// изученных коммутатором MAC-адресов (FDB).
const (
	// oidDot1dBaseBridgeAddress — системный MAC-адрес коммутатора.
	oidDot1dBaseBridgeAddress = "1.3.6.1.2.1.17.1.1.0"

	// oidDot1dBasePortIfIndex — соответствие номера порта моста (bridge port)
	// индексу интерфейса ifIndex.
	oidDot1dBasePortIfIndex = "1.3.6.1.2.1.17.1.4.1.2"

	// oidDot1dTpFdbAddress — MAC-адрес из FDB-таблицы (индекс записи).
	oidDot1dTpFdbAddress = "1.3.6.1.2.1.17.4.3.1.1"

	// oidDot1dTpFdbPort — номер порта моста, на котором изучен MAC-адрес.
	oidDot1dTpFdbPort = "1.3.6.1.2.1.17.4.3.1.2"
)

// ifSpeedUnknown — значение ifSpeed, означающее "скорость неизвестна"
// (0xFFFFFFFF, RFC 2863). Для таких интерфейсов скорость определяется
// по ifHighSpeed (в Мбит/с).
const ifSpeedUnknown uint64 = 0xFFFFFFFF

func init() {
	RegisterCollector(model.CollectTypeSNMP.String(), NewSNMPCollector())
}

// SNMPCollector — реализация сбора информации с коммутатора по протоколу
// SNMP v2c с комьюнити public.
//
// Внутри этой реализации будет развиваться поддержка конкретных
// производителей и моделей по типу сбора из очереди заданий (sw.CollectType):
// SNMP_Cisco, SNMP_Eltex_MAS3032 и т.п. Пока все специализированные типы
// обрабатываются общей реализацией базового типа SNMP, опрашивающей
// стандартные MIB: MIB-II (системная информация), IF-MIB (порты, счётчики)
// и BRIDGE-MIB (FDB-таблица).
type SNMPCollector struct{}

// NewSNMPCollector создаёт SNMP-реализацию сбора информации.
func NewSNMPCollector() *SNMPCollector {
	return &SNMPCollector{}
}

// Name возвращает имя типа сбора данных.
func (c *SNMPCollector) Name() string {
	return model.CollectTypeSNMP.String()
}

// Collect выполняет сбор информации с коммутатора по протоколу SNMP v2c.
// Принимает контекст для отмены и таймаута сбора, полную информацию
// о коммутаторе из очереди заданий и возвращает собранную модель.
func (c *SNMPCollector) Collect(ctx context.Context, sw model.SwInfo) (model.Switch, error) {
	if err := ctx.Err(); err != nil {
		return model.Switch{}, err
	}

	// Создаём подключение к SNMP-агенту коммутатора: SNMP v2c, комьюнити
	// public, стандартный порт 161. Контекст передаётся в библиотеку:
	// отмена или таймаут сбора прерывают сетевые операции и блокирующие
	// вызовы. Максимальное число OID в одном GET-запросе ограничиваем
	// константой библиотеки, чтобы не перегружать агент.
	g := &gosnmp.GoSNMP{
		Target:    sw.IP,
		Port:      snmpPort,
		Community: snmpCommunity,
		Version:   gosnmp.Version2c,
		Timeout:   snmpTimeout,
		Retries:   snmpRetries,
		Context:   ctx,
		MaxOids:   gosnmp.MaxOids,
	}

	if err := g.Connect(); err != nil {
		return model.Switch{}, fmt.Errorf("подключение к SNMP-агенту %s: %w", sw.IP, err)
	}
	defer func() {
		_ = g.Conn.Close()
	}()

	return collectSwitch(ctx, g, sw)
}
