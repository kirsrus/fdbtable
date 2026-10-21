// Этот файл содержит реализацию сбора информации с коммутатора
// через протокол SSH с помощью библиотеки scrapligo: параметры
// подключения, выбор профиля команд по типу сбора и реализацию
// Collector.
package collector

import (
	"context"
	"fmt"
	"time"

	"github.com/kirsrus/fdbtable/internal/model"
	scrapligodriver "github.com/scrapli/scrapligo/driver/generic"
	scrapligooptions "github.com/scrapli/scrapligo/driver/options"
	scraplitransport "github.com/scrapli/scrapligo/transport"
)

// Параметры подключения к SSH-серверу коммутатора.
const (
	// sshPort — стандартный порт SSH-сервера (tcp/22).
	sshPort = 22

	// sshOperationTimeout — таймаут выполнения одной команды на SSH-сессии
	// устройства. Если устройство долго обрабатывает команду (например,
	// при большом выводе), операция завершается ошибкой.
	sshOperationTimeout = 30 * time.Second

	// sshSocketTimeout — таймаут установки TCP-подключения к SSH-серверу.
	sshSocketTimeout = 10 * time.Second

	// sshRateSampleInterval — пауза между двумя замерами счётчиков октетов
	// интерфейсов для вычисления текущей скорости передачи (InRaten/OutRate).
	sshRateSampleInterval = 5 * time.Second

	// sshTermWidth и sshTermHeight — размер псевдотерминала SSH-сессии.
	// Большая ширина предотвращает перенос длинных строк таблиц (FDB,
	// интерфейсы) в выводе, большая высота сокращает страничный вывод.
	sshTermWidth  = 512
	sshTermHeight = 512
)

func init() {
	RegisterCollector(model.CollectTypeSSH.String(), NewSSHCollector())
}

// SSHCollector — реализация сбора информации с коммутатора по протоколу
// SSH с помощью библиотеки scrapligo.
//
// Внутри этой реализации будет развиваться поддержка конкретных
// производителей и моделей по типу сбора из очереди заданий (sw.CollectType):
// SSH_AllietTelesis_908, SSH_Cisco и т.п. Пока все специализированные типы
// обрабатываются общим профилем базового типа SSH (selectSSHProfile),
// выполняющим набор команд и разбирающим вывод в Cisco-подобном формате.
type SSHCollector struct{}

// NewSSHCollector создаёт SSH-реализацию сбора информации.
func NewSSHCollector() *SSHCollector {
	return &SSHCollector{}
}

// Name возвращает имя типа сбора данных.
func (c *SSHCollector) Name() string {
	return model.CollectTypeSSH.String()
}

// Collect выполняет сбор информации с коммутатора по протоколу SSH.
// Принимает контекст для отмены и таймаута сбора, полную информацию
// о коммутаторе из очереди заданий (IP, логин и пароль из sw.Username,
// sw.Password) и возвращает собранную модель.
func (c *SSHCollector) Collect(ctx context.Context, sw model.SwInfo) (model.Switch, error) {
	if err := ctx.Err(); err != nil {
		return model.Switch{}, err
	}

	// Для подключения обязательны логин и пароль: по SSH, в отличие от
	// SNMP, анонимный доступ к командной строке устройства невозможен.
	if sw.Username == "" || sw.Password == "" {
		return model.Switch{}, fmt.Errorf("для сбора по SSH у коммутатора %s не заданы логин или пароль", sw.IP)
	}

	// Профиль сбора данных: команды и парсеры вывода выбираются по типу
	// сбора из очереди заданий (sw.CollectType).
	profile := selectSSHProfile(sw.CollectType)

	// Создаём SSH-подключение к коммутатору через библиотеку scrapligo.
	// Логин и пароль берутся из информации о коммутаторе из очереди
	// заданий. Транспорт "standard" построен на golang.org/x/crypto/ssh
	// (чистый Go, без внешних зависимостей и системного клиента ssh).
	// Проверка ключа хоста отключена: устройства в сети не имеют известных
	// хостов, а ключи сервера могут меняться при замене оборудования.
	// Таймауты ограничивают подключение и каждую отдельную команду.
	driver, err := scrapligodriver.NewDriver(
		sw.IP,
		scrapligooptions.WithAuthUsername(sw.Username),
		scrapligooptions.WithAuthPassword(sw.Password),
		scrapligooptions.WithAuthNoStrictKey(),
		scrapligooptions.WithPort(sshPort),
		scrapligooptions.WithTransportType(scraplitransport.StandardTransport),
		scrapligooptions.WithTimeoutSocket(sshSocketTimeout),
		scrapligooptions.WithTimeoutOps(sshOperationTimeout),
		scrapligooptions.WithTermWidth(sshTermWidth),
		scrapligooptions.WithTermHeight(sshTermHeight),
	)
	if err != nil {
		return model.Switch{}, fmt.Errorf("создание SSH-подключения к %s: %w", sw.IP, err)
	}

	// Открываем сессию: установка TCP-подключения, аутентификация.
	// Сбор ограничен таймаутом, поэтому подключение завершится ошибкой
	// при недоступном устройстве в пределах sshSocketTimeout.
	if err := driver.Open(); err != nil {
		return model.Switch{}, fmt.Errorf("подключение по SSH к %s: %w", sw.IP, err)
	}
	defer func() {
		_ = driver.Close()
	}()

	// Отключаем страничный вывод (--More--), который обрезает большие
	// таблицы (FDB, счётчики). Команда отключения поддерживается не всеми
	// устройствами: ошибка попадёт в вывод команды, но не прервёт сессию.
	for _, cmd := range profile.pagingCmds {
		if _, err := sendInput(ctx, driver, cmd); err != nil {
			break
		}
	}

	return collectSwitchSSH(ctx, driver, sw, profile)
}
