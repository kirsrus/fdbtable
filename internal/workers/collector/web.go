// Этот файл содержит реализацию сбора информации с коммутатора
// через WEB-интерфейс (заглушка).
package collector

import (
	"context"

	"github.com/kirsrus/fdbtable/internal/model"
)

func init() {
	RegisterCollector(TypeWEB, NewWebCollector())
}

// WebCollector — реализация сбора информации с коммутатора через
// WEB-интерфейс (заглушка).
//
// Внутри этой реализации будет развиваться поддержка конкретных
// производителей и моделей по типу сбора из очереди заданий (sw.CollectType):
// WEB_Rujio_808P_v433 и т.п. Пока все специализированные типы
// обрабатываются общей заглушкой базового типа WEB.
type WebCollector struct{}

// NewWebCollector создаёт WEB-реализацию сбора информации.
func NewWebCollector() *WebCollector {
	return &WebCollector{}
}

// Name возвращает имя типа сбора данных.
func (c *WebCollector) Name() string {
	return TypeWEB
}

// Collect выполняет сбор информации с коммутатора через его WEB-интерфейс
// (заглушка). Принимает контекст для отмены и таймаута сбора, полную
// информацию о коммутаторе из очереди заданий и возвращает собранную модель.
func (c *WebCollector) Collect(ctx context.Context, sw model.SwInfo) (model.Switch, error) {
	// TODO: реализовать загрузку страниц WEB-интерфейса и парсинг данных.
	// Для специализированных типов (например, WEB_Rujio_808P_v433) парсить
	// страницы по профилю производителя. Пока — общая заглушка базового типа.
	if err := waitStub(ctx); err != nil {
		return model.Switch{}, err
	}
	return stubSwitch(sw), nil
}
