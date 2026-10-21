// Этот файл содержит функции сбора информации с коммутатора по SSH:
// выбор профиля команд по типу сбора, выполнение команд через библиотеку
// scrapligo и парсинг вывода (системная информация, порты со счётчиками,
// FDB-таблица) с формированием модели Switch.
package collector

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"net"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/kirsrus/fdbtable/internal/model"
	scrapligodriver "github.com/scrapli/scrapligo/driver/generic"
)

// sshProfile — профиль сбора данных по SSH для типа сбора из очереди
// заданий: набор команд для получения системной информации, портов,
// счётчиков и FDB-таблицы.
type sshProfile struct {
	// pagingCmds — команды отключения страничного вывода (--More--),
	// выполняемые сразу после открытия сессии. Обычно несколько команд:
	// при неизвестном устройстве выполняется первая, далее ошибки других
	// команд не прерывают сбор. Пустой список — отключение не требуется.
	pagingCmds []string

	// versionCmd — команда вывода системной информации
	// (например, "show version").
	versionCmd string

	// ifacesCmd — команда вывода состояния портов
	// (например, "show interfaces status").
	ifacesCmd string

	// countersCmd — команда вывода счётчиков октетов интерфейсов
	// (например, "show interfaces counters").
	countersCmd string

	// fdbCmd — команда вывода FDB-таблицы изученных MAC-адресов
	// (например, "show mac address-table").
	fdbCmd string
}

// Команды сбора, используемые общим профилем (Cisco-подобный CLI).
const (
	sshCmdPaging   = "terminal length 0"
	sshCmdVersion  = "show version"
	sshCmdIfaces   = "show interfaces status"
	sshCmdCounters = "show interfaces counters"
	sshCmdFdb      = "show mac address-table"
)

// defaultSSHProfile — общий профиль базового типа SSH. Команды и парсеры
// рассчитаны на Cisco-подобный CLI; для устройств других вендоров профиль
// добавляется в sshProfilesByType.
var defaultSSHProfile = sshProfile{
	pagingCmds:  []string{sshCmdPaging},
	versionCmd:  sshCmdVersion,
	ifacesCmd:   sshCmdIfaces,
	countersCmd: sshCmdCounters,
	fdbCmd:      sshCmdFdb,
}

// sshProfilesByType — специализированные профили сбора по типам из
// очереди заданий. Ключ — нормализованный (верхний регистр) тип сбора.
// Список расширяется по мере добавления поддержки конкретных
// производителей: например, для SSH_Cisco — команды Cisco IOS,
// для SSH_AllietTelesis_908 — команды Allied Telesis AW+ и т.п.
// Пока все специализированные типы обрабатываются общим профилем
// базового типа SSH.
var sshProfilesByType = map[string]sshProfile{}

// selectSSHProfile выбирает профиль сбора данных по типу сбора
// (sw.CollectType). Для базового типа SSH и всех пока нереализованных
// специализированных типов (SSH_Cisco, SSH_AllietTelesis_908 и т.п.)
// возвращается общий профиль базового типа.
func selectSSHProfile(collectType string) sshProfile {
	if profile, ok := sshProfilesByType[normalizeType(collectType)]; ok {
		return profile
	}
	return defaultSSHProfile
}

// sshSystem — системная информация коммутатора, извлечённая из вывода
// команды системной информации по SSH.
type sshSystem struct {
	descr           string
	name            string
	vendor          string
	model           string
	softwareVersion string
	uptime          time.Duration
}

// collectSwitchSSH выполняет сбор информации с коммутатора по SSH и
// формирует модель Switch: системная информация, порты со счётчиками
// и FDB-таблица. Команды выполняются последовательно через одну
// SSH-сессию, отменяемую контекстом сбора.
func collectSwitchSSH(ctx context.Context, driver *scrapligodriver.Driver, sw model.SwInfo, profile sshProfile) (model.Switch, error) {
	swInfo := model.Switch{
		IP:          sw.IP,
		MAC:         sw.MAC,
		Hostname:    sw.Hostname,
		Location:    sw.Location,
		Description: sw.Description,
		CollectedAt: time.Now(),
	}

	// 1. Системная информация.
	sys, err := collectSSHSystem(ctx, driver, profile.versionCmd)
	if err != nil {
		return model.Switch{}, fmt.Errorf("опрос системной информации: %w", err)
	}
	if sys.descr != "" {
		swInfo.Description = sys.descr
	}
	if sys.name != "" {
		swInfo.Hostname = sys.name
	}
	swInfo.Vendor = sys.vendor
	swInfo.Model = sys.model
	swInfo.SoftwareVersion = sys.softwareVersion
	swInfo.Uptime = sys.uptime

	// 2. Таблица портов и счётчики октетов (первый замер).
	ifaces, names, err := collectSSHIfaces(ctx, driver, profile)
	if err != nil {
		return model.Switch{}, fmt.Errorf("опрос состояния портов: %w", err)
	}

	// 3. FDB-таблица: MAC-адреса, изученные на портах.
	macsByPort, err := collectSSHFdb(ctx, driver, profile.fdbCmd)
	if err != nil {
		return model.Switch{}, fmt.Errorf("опрос FDB-таблицы: %w", err)
	}

	// 4. Второй замер счётчиков октетов и расчёт скорости портов.
	if err := rateSSHCounters(ctx, driver, profile.countersCmd, ifaces); err != nil {
		return model.Switch{}, fmt.Errorf("расчёт скорости портов: %w", err)
	}

	// 5. Формируем список портов в порядке их вывода.
	swInfo.Ports = make([]model.Port, 0, len(names))
	for _, name := range names {
		data := ifaces[name]
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
			Macs:       macsByPort[name],
		})
	}
	return swInfo, nil
}

// sendInput отправляет команду на устройство и возвращает очищенный
// вывод (библиотека scrapligo убирает приглашение и управляющие
// символы). Команда выполняется в отдельной горутине: отмена контекста
// сбора прерывает ожидание ответа, а сама операция ограничена таймаутом
// одной команды (sshOperationTimeout) и в худшем случае завершится сама.
func sendInput(ctx context.Context, driver *scrapligodriver.Driver, input string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	type result struct {
		out string
		err error
	}
	ch := make(chan result, 1)
	go func() {
		response, err := driver.SendCommand(input)
		if err != nil {
			ch <- result{err: err}
			return
		}
		// response.Failed сигнализирует, что команда распознана устройством,
		// но завершилась ошибкой (например, "% Invalid input"). Такие выводы
		// парсеры пропускают, поэтому ошибкой сбора это не делаем: сбор
		// продолжается с остальными командами.
		ch <- result{out: response.Result}
	}()

	select {
	case res := <-ch:
		return res.out, res.err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// collectSSHSystem выполняет команду вывода системной информации и
// разбирает её вывод.
func collectSSHSystem(ctx context.Context, driver *scrapligodriver.Driver, cmd string) (sshSystem, error) {
	out, err := sendInput(ctx, driver, cmd)
	if err != nil {
		return sshSystem{}, err
	}
	return parseSSHSystem(out), nil
}

// Регулярные выражения для разбора вывода системной информации.
var (
	// sshModelRe — модель устройства по явной строке "Model Number:".
	sshModelRe = regexp.MustCompile(`(?i)model(?: number)?[:\s]+([A-Za-z][A-Za-z0-9\-]*)`)

	// sshImageRe — имя образа ПО в скобках из первой строки "show version"
	// Cisco: "Cisco IOS Software, C2960S Software (C2960S-UNIVERSALK9-M), ...".
	// Модель — первый токен имени образа.
	sshImageRe = regexp.MustCompile(`\(([A-Za-z0-9]+(?:[-_][A-Za-z0-9]+)*)\)`)

	// sshVersionRe — версия программного обеспечения: "Version 15.2(2)E7".
	sshVersionRe = regexp.MustCompile(`(?i)version\s+([0-9][^\s,]+)`)

	// sshNameRe — имя устройства: "System Name: SW-1".
	sshNameRe = regexp.MustCompile(`(?i)system\s+name[:\s]+(\S+)`)

	// sshUptimeRe — время работы: "Uptime is 1 week, 2 days, 3 hours, 4 minutes".
	sshUptimeRe = regexp.MustCompile(`(?i)uptime is\s+([0-9].*)`)
)

// parseSSHSystem извлекает системную информацию из вывода команды
// (например, "show version"). Вывод устройств разных вендоров сильно
// отличается, поэтому каждое значение извлекается по возможности:
// не найдено — поле остаётся пустым и сбор продолжается.
func parseSSHSystem(out string) sshSystem {
	var sys sshSystem
	for _, rawLine := range strings.Split(out, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}

		// Описание устройства — первая содержательная строка вывода
		// (например, "Cisco IOS Software, C2960S Software ...").
		if sys.descr == "" {
			sys.descr = line
		}

		// Производитель Cisco определяется по типовой первой строке
		// "show version".
		if sys.vendor == "" && strings.Contains(strings.ToLower(line), "cisco ios software") {
			sys.vendor = "Cisco"
		}

		// Модель устройства: явная строка "Model Number:" или первый
		// токен имени образа ПО в скобках.
		if sys.model == "" {
			if m := sshModelRe.FindStringSubmatch(line); m != nil {
				sys.model = m[1]
			} else if m := sshImageRe.FindStringSubmatch(line); m != nil && strings.ContainsAny(m[1], "-_") {
				sys.model = m[1]
				if i := strings.IndexAny(sys.model, "-_"); i > 0 {
					sys.model = sys.model[:i]
				}
			}
		}

		// Версия программного обеспечения.
		if sys.softwareVersion == "" {
			if m := sshVersionRe.FindStringSubmatch(line); m != nil {
				sys.softwareVersion = m[1]
			}
		}

		// Имя устройства.
		if sys.name == "" {
			if m := sshNameRe.FindStringSubmatch(line); m != nil {
				sys.name = m[1]
			}
		}

		// Время работы с последней перезагрузки.
		if sys.uptime == 0 {
			if m := sshUptimeRe.FindStringSubmatch(line); m != nil {
				sys.uptime = parseUptime(m[1])
			}
		}
	}
	return sys
}

// parseUptime разбирает текстовое время работы вида
// "2 weeks, 3 days, 4 hours, 5 minutes" и возвращает time.Duration.
// Неизвестные единицы измерения пропускаются.
func parseUptime(s string) time.Duration {
	var d time.Duration
	for _, part := range strings.Split(s, ",") {
		fields := strings.Fields(part)
		if len(fields) < 2 {
			continue
		}
		value, err := strconv.ParseFloat(strings.TrimSpace(fields[0]), 64)
		if err != nil {
			continue
		}
		unit := strings.ToLower(fields[1])
		switch {
		case strings.HasPrefix(unit, "week"):
			d += time.Duration(value) * 7 * 24 * time.Hour
		case strings.HasPrefix(unit, "day"):
			d += time.Duration(value) * 24 * time.Hour
		case strings.HasPrefix(unit, "hour"):
			d += time.Duration(value) * time.Hour
		case strings.HasPrefix(unit, "minute"):
			d += time.Duration(value) * time.Minute
		case strings.HasPrefix(unit, "second"):
			d += time.Duration(value) * time.Second
		}
	}
	return d
}

// ifaceData (тип определён в snmp_collect.go) переиспользуется для
// хранения сведений об интерфейсах, собранных по SSH: счётчики октетов
// и ошибок, скорость, административное и операционное состояние.

// collectSSHIfaces опрашивает состояние портов и счётчики октетов
// (первый замер) и возвращает сведения об интерфейсах коммутатора,
// а также имена портов в порядке их вывода.
func collectSSHIfaces(ctx context.Context, driver *scrapligodriver.Driver, profile sshProfile) (map[string]*ifaceData, []string, error) {
	statusOut, err := sendInput(ctx, driver, profile.ifacesCmd)
	if err != nil {
		return nil, nil, err
	}
	countersOut, err := sendInput(ctx, driver, profile.countersCmd)
	if err != nil {
		return nil, nil, err
	}
	ifaces, names := parseSSHIfaces(statusOut, countersOut)
	return ifaces, names, nil
}

// column — позиция колонки таблицы в строке вывода.
type column struct {
	start int
	end   int
}

// columnPositions определяет позиции колонок таблицы по строке
// заголовка: имя колонки — это непрерывная последовательность
// непробельных символов, началом колонки считается её первая буква,
// концом — начало следующей колонки (или конец строки).
func columnPositions(header string) (map[string]column, bool) {
	locs := tokenRe.FindAllStringIndex(header, -1)
	if len(locs) == 0 {
		return nil, false
	}
	cols := make(map[string]column, len(locs))
	for i, loc := range locs {
		end := len(header)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		name := header[loc[0]:loc[1]]
		cols[name] = column{start: loc[0], end: end}
	}
	return cols, true
}

// field возвращает значение колонки строки таблицы.
func field(line string, cols map[string]column, name string) string {
	col, ok := cols[name]
	if !ok {
		return ""
	}
	if col.end > len(line) {
		col.end = len(line)
	}
	if col.start >= col.end {
		return ""
	}
	return strings.TrimSpace(line[col.start:col.end])
}

// tokenRe — непробельная последовательность символов (токен строки).
var tokenRe = regexp.MustCompile(`\S+`)

// headerLine ищет в выводе строку заголовка таблицы, содержащую все
// перечисленные колонки.
func headerLine(out string, columns ...string) (string, bool) {
	for _, line := range strings.Split(out, "\n") {
		lower := strings.ToLower(line)
		all := true
		for _, col := range columns {
			if !strings.Contains(lower, strings.ToLower(col)) {
				all = false
				break
			}
		}
		if all {
			return line, true
		}
	}
	return "", false
}

// parseSSHIfaces разбирает вывод команд состояния портов и счётчиков
// октетов Cisco-подобного формата: "show interfaces status" (колонки
// Port/Name/Status/Vlan/Duplex/Speed/Type) и "show interfaces counters"
// (разделы InOctets/OutOctets). Возвращает сведения об интерфейсах и
// имена портов в порядке вывода.
func parseSSHIfaces(statusOut, countersOut string) (map[string]*ifaceData, []string) {
	// Заголовок таблицы состояния портов: строка с колонками Port,
	// Status и Speed.
	header, ok := headerLine(statusOut, "Port", "Status", "Speed")
	if !ok {
		return nil, nil
	}
	cols, ok := columnPositions(header)
	if !ok {
		return nil, nil
	}

	counters := parseSSHCounters(countersOut)

	ifaces := make(map[string]*ifaceData)
	var names []string
	for _, rawLine := range strings.Split(statusOut, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.Contains(line, "---") || line == header {
			continue
		}

		name := field(rawLine, cols, "Port")
		if name == "" {
			continue
		}

		// Индекс порта присваиваем по порядку вывода (начиная с 1):
		// в CLI-выводе, в отличие от SNMP, нет явного ifIndex.
		data := &ifaceData{
			index: int64(len(names)) + 1,
			name:  name,
		}

		// Административное и операционное состояние порта.
		admin, oper := parseIfState(field(rawLine, cols, "Status"))
		data.admin = int64(admin)
		data.oper = int64(oper)

		// Скорость порта в Мбит/с ("a-1000", "1000", "auto" и т.п.).
		data.speed = parseIfSpeed(field(rawLine, cols, "Speed"))

		// Счётчики октетов и ошибок (первый замер).
		if c, ok := counters[name]; ok {
			data.inOctets = c.inOctets
			data.inErrors = c.inErrors
			data.outOctets = c.outOctets
			data.outErrors = c.outErrors
		}

		ifaces[name] = data
		names = append(names, name)
	}
	return ifaces, names
}

// parseIfState определяет административное и операционное состояние
// порта по тексту колонки Status таблицы "show interfaces status":
//
//	disabled      — порт административно выключен;
//	connected     — порт поднят и работает;
//	notconnect    — порт включён, но кабель не подключён;
//	err-disabled  — порт отключён из-за ошибки;
//	прочее        — порт включён, операционное состояние неизвестно.
func parseIfState(status string) (model.AdminState, model.OperState) {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "disabled", "disable":
		return model.AdminDown, model.OperDown
	case "connected", "up", "link up", "established":
		return model.AdminUp, model.OperUp
	case "notconnect", "not connected", "down", "link down", "no-link":
		return model.AdminUp, model.OperDown
	case "err-disabled", "error-disabled", "err-disable":
		return model.AdminUp, model.OperDown
	default:
		return model.AdminUp, model.OperUnknown
	}
}

// sshSpeedRe — числа внутри значения скорости порта: "a-1000" → "1000",
// "10/100" → "10", "100".
var sshSpeedRe = regexp.MustCompile(`(\d+)`)

// parseIfSpeed извлекает скорость порта в Мбит/с из текста колонки
// Speed таблицы состояния портов. Для значений вида "10/100" (схема
// скоростей) берётся максимальная из перечисленных.
func parseIfSpeed(s string) int64 {
	if strings.EqualFold(strings.TrimSpace(s), "auto") {
		return 0
	}
	var max int64
	for _, n := range sshSpeedRe.FindAllString(s, -1) {
		if v, err := strconv.ParseInt(n, 10, 64); err == nil && v > max {
			max = v
		}
	}
	return max
}

// sshCounters — счётчики октетов и ошибок порта из вывода команд
// счётчиков.
type sshCounters struct {
	inOctets  uint64
	inErrors  uint64
	outOctets uint64
	outErrors uint64
}

// parseSSHCounters разбирает вывод команды счётчиков (Cisco-подобный
// формат "show interfaces counters"): разделы со счётчиками приёма
// (InOctets), передачи (OutOctets) и ошибок (InErrors/OutErrors).
// Для каждой строки порт — первый токен, значение счётчика — первый
// числовой столбец. Разделы определяются по заголовкам колонок.
func parseSSHCounters(out string) map[string]sshCounters {
	counters := make(map[string]sshCounters)
	section := ""
	for _, rawLine := range strings.Split(out, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}
		// Заголовок раздела.
		switch {
		case strings.Contains(line, "InOctets"):
			section = "in"
			continue
		case strings.Contains(line, "InErrors"):
			section = "inerr"
			continue
		case strings.Contains(line, "OutOctets"):
			section = "out"
			continue
		case strings.Contains(line, "OutErrors"):
			section = "outerr"
			continue
		}
		if section == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		value, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			continue
		}
		data := counters[fields[0]]
		switch section {
		case "in":
			data.inOctets = value
		case "inerr":
			data.inErrors = value
		case "out":
			data.outOctets = value
		case "outerr":
			data.outErrors = value
		}
		counters[fields[0]] = data
	}
	return counters
}

// collectSSHFdb опрашивает FDB-таблицу коммутатора и возвращает карту
// "имя порта" → список изученных MAC-адресов (отсортированный).
func collectSSHFdb(ctx context.Context, driver *scrapligodriver.Driver, cmd string) (map[string][]net.HardwareAddr, error) {
	out, err := sendInput(ctx, driver, cmd)
	if err != nil {
		return nil, err
	}
	return parseSSHFdb(out), nil
}

// parseSSHFdb разбирает вывод FDB-таблицы (например,
// "show mac address-table"). Строка записи содержит MAC-адрес и имя
// порта: "1    0050.7966.6800    DYNAMIC     Gi1/0/1". Поддерживаются
// форматы MAC-адресов cisco (aaaa.bbbb.cccc), классический
// (aa:bb:cc:dd:ee:ff) и дефисный (aa-bb-cc-dd-ee-ff). Порт — последний
// токен строки.
func parseSSHFdb(out string) map[string][]net.HardwareAddr {
	macsByPort := make(map[string][]net.HardwareAddr)
	for _, rawLine := range strings.Split(out, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.Contains(line, "---") {
			continue
		}
		mac, ok := parseMAC(line)
		if !ok {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		port := fields[len(fields)-1]
		if port == "" {
			continue
		}
		macsByPort[port] = append(macsByPort[port], mac)
	}
	// Упорядочиваем адреса каждого порта.
	for _, macs := range macsByPort {
		sort.Slice(macs, func(i, j int) bool {
			return bytes.Compare(macs[i], macs[j]) < 0
		})
	}
	return macsByPort
}

// parseMAC находит MAC-адрес в строке вывода и возвращает его в
// каноническом представлении net.HardwareAddr. Форматы: cisco
// (aaaa.bbbb.cccc), классический (aa:bb:cc:dd:ee:ff) и дефисный
// (aa-bb-cc-dd-ee-ff).
func parseMAC(text string) (net.HardwareAddr, bool) {
	for _, token := range strings.Fields(text) {
		clean := strings.NewReplacer(".", "", ":", "", "-", "").Replace(token)
		if len(clean) != 12 {
			continue
		}
		bytes, err := hex.DecodeString(clean)
		if err != nil {
			continue
		}
		return net.HardwareAddr(bytes), true
	}
	return nil, false
}

// rateSSHCounters выполняет второй замер счётчиков октетов интерфейсов
// после паузы sshRateSampleInterval и вычисляет текущую скорость
// передачи (InRaten/OutRate) по приращению счётчиков за время между
// замерами. Пауза прерывается отменой контекста.
func rateSSHCounters(ctx context.Context, driver *scrapligodriver.Driver, cmd string, ifaces map[string]*ifaceData) error {
	timer := time.NewTimer(sshRateSampleInterval)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
		return ctx.Err()
	}

	out, err := sendInput(ctx, driver, cmd)
	if err != nil {
		return fmt.Errorf("повторный опрос счётчиков: %w", err)
	}
	counters := parseSSHCounters(out)
	for name, data := range ifaces {
		cur, ok := counters[name]
		if !ok {
			continue
		}
		data.inRate = octetsRate(data.inOctets, cur.inOctets, sshRateSampleInterval)
		data.outRate = octetsRate(data.outOctets, cur.outOctets, sshRateSampleInterval)
	}
	return nil
}
