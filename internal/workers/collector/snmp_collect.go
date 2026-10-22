// Этот файл содержит функции опроса SNMP-агента коммутатора и
// формирования модели Switch по результатам опроса: системная информация
// (MIB-II, BRIDGE-MIB), таблица интерфейсов (IF-MIB), FDB-таблица
// (BRIDGE-MIB) и расчёт скорости портов по двум замерам счётчиков.
package collector

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/endobit/oui"
	"github.com/gosnmp/gosnmp"
	"github.com/kirsrus/fdbtable/internal/model"
)

// system — системная информация коммутатора из MIB-II и BRIDGE-MIB.
type system struct {
	descr     string
	objectID  string
	uptime    time.Duration
	contact   string
	name      string
	location  string
	bridgeMAC string
}

// ifaceData — собранные по SNMP данные интерфейса.
type ifaceData struct {
	index      int64
	name       string
	inOctets   uint64
	inErrors   uint64
	outOctets  uint64
	outErrors  uint64
	speed      int64 // Мбит/с
	admin      int64
	oper       int64
	lastChange time.Duration
	inRate     int64 // бит/с, рассчитан по двум замерам счётчиков
	outRate    int64 // бит/с, рассчитан по двум замерам счётчиков
}

// collectSwitch выполняет опрос коммутатора по SNMP и формирует модель
// Switch: системная информация, порты со счётчиками и FDB-таблица.
func collectSwitch(ctx context.Context, conn *gosnmp.GoSNMP, sw model.SwInfo) (model.Switch, error) {
	swInfo := model.Switch{
		IP:          sw.IP,
		MAC:         sw.MAC,
		Hostname:    sw.Hostname,
		Location:    sw.Location,
		Description: sw.Description,
		CollectedAt: time.Now(),
	}

	// 1. Системная информация MIB-II и адрес моста BRIDGE-MIB.
	sys, err := collectSystem(conn)
	if err != nil {
		return model.Switch{}, fmt.Errorf("опрос системной информации: %w", err)
	}
	if sys.descr != "" {
		swInfo.Description = sys.descr
	}
	swInfo.ObjectID = sys.objectID
	if sys.name != "" {
		swInfo.Hostname = sys.name
	}
	if sys.location != "" {
		swInfo.Location = sys.location
	}
	swInfo.Contact = sys.contact
	swInfo.Uptime = sys.uptime
	if sys.bridgeMAC != "" {
		swInfo.MAC = sys.bridgeMAC
	}
	if sys.objectID != "" || sys.descr != "" {
		swInfo.Vendor, swInfo.Model = vendorAndModel(sys.objectID, sys.descr)
	}

	// 2. Таблица интерфейсов ifTable/ifXTable.
	ifaces, indexes, err := collectIfaces(conn)
	if err != nil {
		return model.Switch{}, fmt.Errorf("опрос таблицы интерфейсов: %w", err)
	}

	// 3. FDB-таблица: MAC-адреса, изученные на портах.
	macsByIf, err := collectFdb(conn, ifaces)
	if err != nil {
		return model.Switch{}, fmt.Errorf("опрос FDB-таблицы: %w", err)
	}

	// 4. Второй замер счётчиков октетов и расчёт скорости портов.
	if err := rateCounters(ctx, conn, ifaces); err != nil {
		return model.Switch{}, fmt.Errorf("расчёт скорости портов: %w", err)
	}

	// 5. Формируем список портов в порядке возрастания индексов.
	swInfo.Ports = make([]model.Port, 0, len(indexes))
	for _, idx := range indexes {
		data := ifaces[idx]
		swInfo.Ports = append(swInfo.Ports, model.Port{
			Index:      int(data.index),
			Name:       data.name,
			AdminState: model.AdminState(data.admin),
			OperState:  model.OperState(data.oper),
			Speed:      data.speed,
			InRaten:    data.inRate,
			OutRate:    data.outRate,
			InErrors:   data.inErrors,
			OutErrors:  data.outErrors,
			LastChange: data.lastChange,
			Macs:       macsByIf[idx],
		})
	}
	return swInfo, nil
}

// collectSystem опрашивает системную группу MIB-II (sysDescr, sysObjectID,
// sysUpTime, sysContact, sysName, sysLocation) и системный адрес моста
// BRIDGE-MIB (dot1dBaseBridgeAddress).
func collectSystem(conn *gosnmp.GoSNMP) (system, error) {
	values, err := get(conn,
		oidSysDescr, oidSysObjectID, oidSysUpTime,
		oidSysContact, oidSysName, oidSysLocation,
		oidDot1dBaseBridgeAddress,
	)
	if err != nil {
		return system{}, err
	}

	var sys system
	if pdu, ok := values[oidSysDescr]; ok {
		sys.descr = pduToString(pdu)
	}
	if pdu, ok := values[oidSysObjectID]; ok {
		sys.objectID = pduToString(pdu)
	}
	if pdu, ok := values[oidSysUpTime]; ok {
		// TimeTicks измеряется в сотых долях секунды.
		sys.uptime = time.Duration(pduToUint64(pdu)) * 10 * time.Millisecond
	}
	if pdu, ok := values[oidSysContact]; ok {
		sys.contact = pduToString(pdu)
	}
	if pdu, ok := values[oidSysName]; ok {
		sys.name = pduToString(pdu)
	}
	if pdu, ok := values[oidSysLocation]; ok {
		sys.location = pduToString(pdu)
	}
	if pdu, ok := values[oidDot1dBaseBridgeAddress]; ok {
		if mac, ok := pduToMAC(pdu); ok {
			sys.bridgeMAC = mac.String()
		}
	}
	return sys, nil
}

// collectIfaces опрашивает таблицы интерфейсов ifTable/ifXTable и
// возвращает сведения об интерфейсах коммутатора, а также отсортированные
// по возрастанию индексы интерфейсов.
func collectIfaces(conn *gosnmp.GoSNMP) (map[int64]*ifaceData, []int64, error) {
	// Первая колонка ifTable (ifIndex) — источник списка интерфейсов.
	idxColumn, err := walkColumn(conn, oidIfIndex)
	if err != nil {
		return nil, nil, err
	}
	indexes := make([]int64, 0, len(idxColumn))
	for idx := range idxColumn {
		indexes = append(indexes, idx)
	}
	sort.Slice(indexes, func(i, j int) bool { return indexes[i] < indexes[j] })

	// Опрашиваем остальные колонки ifTable и ifXTable.
	descrColumn, err := walkColumn(conn, oidIfDescr)
	if err != nil {
		return nil, nil, err
	}
	nameColumn, err := walkColumn(conn, oidIfName)
	if err != nil {
		return nil, nil, err
	}
	speedColumn, err := walkColumn(conn, oidIfSpeed)
	if err != nil {
		return nil, nil, err
	}
	highSpeedColumn, err := walkColumn(conn, oidIfHighSpeed)
	if err != nil {
		return nil, nil, err
	}
	adminColumn, err := walkColumn(conn, oidIfAdminStatus)
	if err != nil {
		return nil, nil, err
	}
	operColumn, err := walkColumn(conn, oidIfOperStatus)
	if err != nil {
		return nil, nil, err
	}
	lastChangeColumn, err := walkColumn(conn, oidIfLastChange)
	if err != nil {
		return nil, nil, err
	}
	inOctetsColumn, err := walkColumn(conn, oidIfInOctets)
	if err != nil {
		return nil, nil, err
	}
	inErrorsColumn, err := walkColumn(conn, oidIfInErrors)
	if err != nil {
		return nil, nil, err
	}
	outOctetsColumn, err := walkColumn(conn, oidIfOutOctets)
	if err != nil {
		return nil, nil, err
	}
	outErrorsColumn, err := walkColumn(conn, oidIfOutErrors)
	if err != nil {
		return nil, nil, err
	}

	ifaces := make(map[int64]*ifaceData, len(indexes))
	for _, idx := range indexes {
		data := &ifaceData{index: idx}

		if pdu, ok := descrColumn[idx]; ok {
			data.name = pduToString(pdu)
		}
		if pdu, ok := nameColumn[idx]; ok {
			// ifName предпочтительнее ifDescr: содержит краткое имя порта
			// (например, "Gi1/0/1"), а не полное описание.
			if name := pduToString(pdu); name != "" {
				data.name = name
			}
		}
		if pdu, ok := speedColumn[idx]; ok {
			if speed := pduToUint64(pdu); speed != ifSpeedUnknown {
				// ifSpeed измеряется в бит/с, приводим к Мбит/с.
				data.speed = int64(speed / 1_000_000)
			}
		}
		if pdu, ok := highSpeedColumn[idx]; ok {
			// ifHighSpeed измеряется в Мбит/с. Дополняет ifSpeed: для скоростей
			// свыше 100 Мбит/с агенты часто возвращают в ifSpeed 0xFFFFFFFF.
			if highSpeed := pduToUint64(pdu); data.speed <= 0 && highSpeed > 0 {
				data.speed = int64(highSpeed)
			}
		}
		if pdu, ok := adminColumn[idx]; ok {
			data.admin = pduToInt64(pdu)
		}
		if pdu, ok := operColumn[idx]; ok {
			data.oper = pduToInt64(pdu)
		}
		if pdu, ok := lastChangeColumn[idx]; ok {
			// TimeTicks измеряется в сотых долях секунды.
			data.lastChange = time.Duration(pduToUint64(pdu)) * 10 * time.Millisecond
		}
		if pdu, ok := inOctetsColumn[idx]; ok {
			data.inOctets = pduToUint64(pdu)
		}
		if pdu, ok := inErrorsColumn[idx]; ok {
			data.inErrors = pduToUint64(pdu)
		}
		if pdu, ok := outOctetsColumn[idx]; ok {
			data.outOctets = pduToUint64(pdu)
		}
		if pdu, ok := outErrorsColumn[idx]; ok {
			data.outErrors = pduToUint64(pdu)
		}

		ifaces[idx] = data
	}
	return ifaces, indexes, nil
}

// walkColumn обходит колонку таблицы интерфейсов и возвращает карту
// "индекс интерфейса" → значение. Индекс извлекается из последнего
// компонента OID записи.
func walkColumn(conn *gosnmp.GoSNMP, root string) (map[int64]gosnmp.SnmpPDU, error) {
	pdus, err := walk(conn, root)
	if err != nil {
		return nil, err
	}
	column := make(map[int64]gosnmp.SnmpPDU, len(pdus))
	for _, pdu := range pdus {
		idx, ok := lastIndex(pdu.Name)
		if !ok {
			continue
		}
		column[idx] = pdu
	}
	return column, nil
}

// collectFdb опрашивает FDB-таблицу BRIDGE-MIB (dot1dTpFdb) и сопоставляет
// изученные коммутатором MAC-адреса с интерфейсами. Возвращает карту
// "индекс интерфейса" → список MAC-адресов (отсортированный).
func collectFdb(conn *gosnmp.GoSNMP, ifaces map[int64]*ifaceData) (map[int64][]model.Mac, error) {
	// Колонки FDB-таблицы: изученный адрес и порт моста, на котором он
	// изучен. Индекс записи в обеих колонках — MAC-адрес.
	addrPdus, err := walk(conn, oidDot1dTpFdbAddress)
	if err != nil {
		return nil, fmt.Errorf("опрос FDB-таблицы: %w", err)
	}
	portPdus, err := walk(conn, oidDot1dTpFdbPort)
	if err != nil {
		return nil, fmt.Errorf("опрос FDB-таблицы: %w", err)
	}

	// Устройство без стандартной FDB-таблицы BRIDGE-MIB: данные собрать
	// невозможно, для этого понадобятся вендорские MIB специализированных
	// реализаций.
	if len(addrPdus) == 0 && len(portPdus) == 0 {
		return nil, fmt.Errorf("коммутатор не поддерживает FDB-таблицу BRIDGE-MIB (dot1dTpFdb)")
	}

	addrByKey := make(map[string]net.HardwareAddr, len(addrPdus))
	for _, pdu := range addrPdus {
		mac, ok := macFromOid(pdu.Name)
		if !ok {
			continue
		}
		key, _ := macKeyFromOid(pdu.Name)
		addrByKey[key] = mac
	}
	portByKey := make(map[string]int64, len(portPdus))
	for _, pdu := range portPdus {
		key, ok := macKeyFromOid(pdu.Name)
		if !ok {
			continue
		}
		portByKey[key] = pduToInt64(pdu)
	}
	if len(addrByKey) == 0 {
		// Адресная колонка пуста — сопоставлять нечего.
		return map[int64][]model.Mac{}, nil
	}

	// Соответствие номера порта моста (bridge port) индексу интерфейса.
	// Таблица dot1dBasePortIfIndex есть не на всех устройствах.
	portToIf := make(map[int64]int64)
	if baseColumn, err := walkColumn(conn, oidDot1dBasePortIfIndex); err == nil {
		for bridgePort, pdu := range baseColumn {
			portToIf[bridgePort] = pduToInt64(pdu)
		}
	}
	// Если соответствие неизвестно, пробуем использовать номер порта моста
	// как ifIndex (на многих устройствах они совпадают).
	portToIfIndex := func(bridgePort int64) (int64, bool) {
		if ifIndex, ok := portToIf[bridgePort]; ok && ifIndex > 0 {
			return ifIndex, true
		}
		if _, ok := ifaces[bridgePort]; ok {
			return bridgePort, true
		}
		return 0, false
	}

	// Группируем адреса по индексам интерфейсов.
	macsByIf := make(map[int64][]model.Mac)
	for key, item := range addrByKey {
		bridgePort, ok := portByKey[key]
		if !ok || bridgePort == 0 {
			// Запись без порта или на порту управления (0) в таблицу портов
			// не попадает.
			continue
		}
		ifIndex, ok := portToIfIndex(bridgePort)
		if !ok {
			continue
		}

		mac := model.Mac{
			Mac:       item,
			StringMac: strings.ToUpper(item.String()),
			Vendor:    oui.VendorFromMAC(item),
		}

		macsByIf[ifIndex] = append(macsByIf[ifIndex], mac)
	}
	// Упорядочиваем адреса каждого порта.
	for _, macs := range macsByIf {
		sort.Slice(macs, func(i, j int) bool {
			return bytes.Compare(macs[i].Mac, macs[j].Mac) < 0
		})
	}
	return macsByIf, nil
}

// rateCounters выполняет второй замер счётчиков октетов интерфейсов после
// паузы snmpRateSampleInterval и вычисляет текущую скорость передачи
// (InRaten/OutRate) по приращению счётчиков за время между замерами.
// Пауза прерывается отменой контекста.
func rateCounters(ctx context.Context, conn *gosnmp.GoSNMP, ifaces map[int64]*ifaceData) error {
	// Пауза между замерами, прерываемая отменой контекста.
	timer := time.NewTimer(snmpRateSampleInterval)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
		return ctx.Err()
	}

	// Второй замер счётчиков приёма и передачи.
	inColumn, err := walkColumn(conn, oidIfInOctets)
	if err != nil {
		return fmt.Errorf("повторный опрос счётчиков приёма: %w", err)
	}
	outColumn, err := walkColumn(conn, oidIfOutOctets)
	if err != nil {
		return fmt.Errorf("повторный опрос счётчиков передачи: %w", err)
	}

	for idx, data := range ifaces {
		if pdu, ok := inColumn[idx]; ok {
			data.inRate = octetsRate(data.inOctets, pduToUint64(pdu), snmpRateSampleInterval)
		}
		if pdu, ok := outColumn[idx]; ok {
			data.outRate = octetsRate(data.outOctets, pduToUint64(pdu), snmpRateSampleInterval)
		}
	}
	return nil
}

// octetsRate вычисляет скорость передачи в бит/с по приращению счётчика
// октетов (в байтах) между двумя замерами с интервалом elapsed. Учитывает
// переполнение 32-битного счётчика Counter32 (RFC 2863).
func octetsRate(prev, cur uint64, elapsed time.Duration) int64 {
	if elapsed <= 0 {
		return 0
	}
	if cur < prev {
		// Счётчик переполнился: Counter32 обнуляется после 2^32 октетов.
		cur += 1 << 32
	}
	delta := cur - prev
	return int64(float64(delta) * 8 / elapsed.Seconds())
}

// vendorByEnterprise — известные номера предприятий (enterprise) из
// sysObjectID и соответствующие производители сетевого оборудования.
// Список расширяется по мере добавления специализированных типов сбора.
var vendorByEnterprise = map[string]string{
	"9":     "Cisco",
	"11":    "HP",
	"171":   "D-Link",
	"207":   "Allied Telesis",
	"890":   "Zyxel",
	"11863": "TP-Link",
	"14988": "MikroTik",
	"1916":  "Extreme Networks",
	"2011":  "Huawei",
	"25506": "H3C",
	"2636":  "Juniper",
	"35265": "Eltex",
}

// enterpriseFromOID возвращает номер предприятия (enterprise) из sysObjectID.
// Например, для "1.3.6.1.4.1.9.1.123" вернёт "9".
func enterpriseFromOID(oid string) string {
	const enterpriseRoot = "1.3.6.1.4.1."
	oid = strings.TrimPrefix(oid, ".")
	if !strings.HasPrefix(oid, enterpriseRoot) {
		return ""
	}
	rest := strings.TrimPrefix(oid, enterpriseRoot)
	if i := strings.IndexByte(rest, '.'); i > 0 {
		rest = rest[:i]
	}
	return rest
}

// vendorAndModel определяет производителя и модель коммутатора по
// sysObjectID и sysDescr. Производитель определяется по номеру предприятия
// в sysObjectID, модель — эвристически по sysDescr: sysDescr обычно
// начинается с названия производителя, а модель — следующий токен
// (например, "D-Link DES-1210-28 ..." → модель "DES-1210-28").
func vendorAndModel(objectID, descr string) (vendor, model string) {
	if enterprise := enterpriseFromOID(objectID); enterprise != "" {
		vendor = vendorByEnterprise[enterprise]
	}
	if vendor == "" || descr == "" {
		return vendor, model
	}

	fields := strings.Fields(descr)
	switch {
	case len(fields) >= 2 && strings.EqualFold(fields[0], vendor):
		model = fields[1]
	case len(fields) >= 3 && strings.EqualFold(fields[0]+" "+fields[1], vendor):
		// Производители из двух слов, например "Allied Telesis".
		model = fields[2]
	}
	return vendor, model
}

// get выполняет SNMP GET по списку OID'ов и возвращает карту
// "OID без ведущей точки" → значение. Ответы-ошибки (нет такого объекта,
// экземпляра или конец MIB, Null) в карту не попадают.
func get(conn *gosnmp.GoSNMP, oids ...string) (map[string]gosnmp.SnmpPDU, error) {
	result, err := conn.Get(oids)
	if err != nil {
		return nil, err
	}
	values := make(map[string]gosnmp.SnmpPDU, len(result.Variables))
	for _, pdu := range result.Variables {
		switch pdu.Type {
		case gosnmp.NoSuchObject, gosnmp.NoSuchInstance, gosnmp.EndOfMibView, gosnmp.Null:
			continue
		}
		values[strings.TrimPrefix(pdu.Name, ".")] = pdu
	}
	return values, nil
}

// walk обходит поддерево MIB от корня root, перебирая значения через
// GETBULK. Если агент не поддерживает GETBULK, обход повторяется через
// GETNEXT (Walk). Записи с типами NoSuchObject/NoSuchInstance/EndOfMibView
// пропускаются.
func walk(conn *gosnmp.GoSNMP, root string) ([]gosnmp.SnmpPDU, error) {
	pdus, err := walkWith(conn.BulkWalk, root)
	if err != nil {
		// Часть агентов не поддерживает GETBULK — обходим поддерево через
		// GETNEXT.
		pdus, err = walkWith(conn.Walk, root)
	}
	return pdus, err
}

// walkWith обходит поддерево указанным методом обхода (BulkWalk или Walk).
func walkWith(walkFn func(rootOid string, walkFn gosnmp.WalkFunc) error, root string) ([]gosnmp.SnmpPDU, error) {
	var pdus []gosnmp.SnmpPDU
	err := walkFn(root, func(pdu gosnmp.SnmpPDU) error {
		switch pdu.Type {
		case gosnmp.NoSuchObject, gosnmp.NoSuchInstance, gosnmp.EndOfMibView:
			return nil
		}
		pdus = append(pdus, pdu)
		return nil
	})
	return pdus, err
}

// pduToString возвращает строковое представление значения PDU.
// Для строковых типов (OctetString и др.) возвращается текст без
// обрамляющих пробелов, для ObjectIdentifier — OID без ведущей точки.
func pduToString(pdu gosnmp.SnmpPDU) string {
	switch pdu.Type {
	case gosnmp.OctetString, gosnmp.ObjectDescription:
		if b, ok := pdu.Value.([]byte); ok {
			return strings.TrimSpace(string(b))
		}
	case gosnmp.ObjectIdentifier:
		if s, ok := pdu.Value.(string); ok {
			return strings.TrimPrefix(s, ".")
		}
	}
	return ""
}

// pduToUint64 возвращает беззнаковое целое значение PDU.
func pduToUint64(pdu gosnmp.SnmpPDU) uint64 {
	n := gosnmp.ToBigInt(pdu.Value)
	if n == nil || n.Sign() < 0 {
		return 0
	}
	return n.Uint64()
}

// pduToInt64 возвращает знаковое целое значение PDU.
func pduToInt64(pdu gosnmp.SnmpPDU) int64 {
	n := gosnmp.ToBigInt(pdu.Value)
	if n == nil {
		return 0
	}
	return n.Int64()
}

// pduToMAC извлекает аппаратный адрес из PDU. В BRIDGE-MIB адрес
// возвращается как OctetString (шесть байт) или ObjectIdentifier.
func pduToMAC(pdu gosnmp.SnmpPDU) (net.HardwareAddr, bool) {
	switch pdu.Type {
	case gosnmp.OctetString:
		if b, ok := pdu.Value.([]byte); ok && len(b) == 6 {
			return net.HardwareAddr(b), true
		}
	case gosnmp.ObjectIdentifier:
		if s, ok := pdu.Value.(string); ok {
			if mac, ok := macFromOid(s); ok {
				return mac, true
			}
		}
	}
	return nil, false
}

// lastIndex возвращает последний компонент OID как целое число.
// Используется для извлечения индекса интерфейса из OID ifTable.
func lastIndex(oid string) (int64, bool) {
	parts := strings.Split(strings.TrimPrefix(oid, "."), ".")
	if len(parts) == 0 {
		return 0, false
	}
	n, err := strconv.ParseInt(parts[len(parts)-1], 10, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}

// macFromOid извлекает MAC-адрес из OID FDB-записи. В BRIDGE-MIB индекс
// записи таблицы dot1dTpFdb — MAC-адрес, закодированный шестью десятичными
// компонентами OID (каждый компонент — один байт адреса).
func macFromOid(oid string) (net.HardwareAddr, bool) {
	parts := strings.Split(strings.TrimPrefix(oid, "."), ".")
	if len(parts) < 6 {
		return nil, false
	}
	addr := make(net.HardwareAddr, 6)
	for i := range addr {
		n, err := strconv.ParseUint(parts[len(parts)-6+i], 10, 8)
		if err != nil {
			return nil, false
		}
		addr[i] = byte(n)
	}
	return addr, true
}

// macKeyFromOid возвращает строковый ключ MAC-адреса из OID FDB-записи —
// последние шесть компонентов OID. Используется для сопоставления записей
// колонок dot1dTpFdbAddress и dot1dTpFdbPort одной таблицы.
func macKeyFromOid(oid string) (string, bool) {
	parts := strings.Split(strings.TrimPrefix(oid, "."), ".")
	if len(parts) < 6 {
		return "", false
	}
	return strings.Join(parts[len(parts)-6:], "."), true
}
