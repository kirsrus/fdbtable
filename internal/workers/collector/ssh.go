// Этот файл содержит реализацию сбора информации с коммутатора
// через SSH (заглушка).
package collector

import (
	"context"

	"github.com/kirsrus/fdbtable/internal/model"
)

func init() {
	RegisterCollector(TypeSSH, NewSSHCollector())
}

// SSHCollector — реализация сбора информации с коммутатора через SSH
// (заглушка).
//
// Внутри этой реализации будет развиваться поддержка конкретных
// производителей и моделей по типу сбора из очереди заданий (sw.CollectType):
// SSH_AllietTelesis_908, SSH_Cisco и т.п. Пока все специализированные типы
// обрабатываются общей заглушкой базового типа SSH.
type SSHCollector struct{}

// NewSSHCollector создаёт SSH-реализацию сбора информации.
func NewSSHCollector() *SSHCollector {
	return &SSHCollector{}
}

// Name возвращает имя типа сбора данных.
func (c *SSHCollector) Name() string {
	return TypeSSH
}

// Collect выполняет сбор информации с коммутатора через SSH (заглушка).
// Принимает контекст для отмены и таймаута сбора, полную информацию
// о коммутаторе из очереди заданий и возвращает собранную модель.
func (c *SSHCollector) Collect(ctx context.Context, sw model.SwInfo) (model.Switch, error) {
	// TODO: реализовать подключение по SSH и парсинг вывода команд.
	// Для специализированных типов (например, SSH_AllietTelesis_908) парсить
	// вывод по профилю производителя. Пока — общая заглушка базового типа.
	if err := waitStub(ctx); err != nil {
		return model.Switch{}, err
	}
	return stubSwitch(sw), nil
}
