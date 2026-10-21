// Этот файл содержит общие вспомогательные функции заглушечных реализаций
// сбора информации: эмуляцию времени опроса и построение модели
// коммутатора-заглушки по данным из очереди заданий.
package collector

import (
	"context"
	"time"

	"github.com/kirsrus/fdbtable/internal/model"
	"github.com/kirsrus/fdbtable/internal/workers/web"
)

// stubCollectDelay — задержка, эмулирующая время реального сбора данных
// заглушечной реализацией. Меньше DefaultCollectTimeout, поэтому сбор
// завершается успешно, если не наступила отмена контекста воркера.
const stubCollectDelay = 15 * time.Second

// waitStub блокирует выполнение на время эмуляции сбора данных и
// прерывается отменой или таймаутом контекста.
func waitStub(ctx context.Context) error {
	select {
	case <-time.After(stubCollectDelay):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// stubSwitch формирует модель коммутатора для заглушечных реализаций.
// Заполняет базовые поля из информации о коммутаторе, пришедшей в очереди
// заданий. Реальные реализации будут заполнять поля данными опроса.
func stubSwitch(sw web.SwInfo) model.Switch {
	return model.Switch{
		IP:          sw.IP,
		MAC:         sw.MAC,
		Hostname:    sw.Hostname,
		Location:    sw.Location,
		Description: sw.Description,
		CollectedAt: time.Now(),
	}
}
