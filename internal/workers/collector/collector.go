// Package collector содержит воркер сбора информации с сетевых коммутаторов.
package collector

import (
	"context"
	"log/slog"

	bolt "go.etcd.io/bbolt"
)

// Worker — воркер сбора информации с сетевых коммутаторов.
type Worker struct {
	db        *bolt.DB
	log       *slog.Logger
	collectCh <-chan string
}

// New создаёт новый воркер сбора информации.
// Принимает базу данных, логгер и канал команд на сбор информации
// (канал на чтение: команды приходят от web-воркера).
func New(db *bolt.DB, log *slog.Logger, collectCh <-chan string) *Worker {
	return &Worker{
		db:        db,
		log:       log,
		collectCh: collectCh,
	}
}

// Run запускает воркер и блокируется до завершения его работы.
// Пока это заглушка: получает команды на сбор информации и логирует их.
func (w *Worker) Run(ctx context.Context) error {
	w.log.Info("запуск воркера сбора информации о коммутаторах")

	for {
		select {
		case <-ctx.Done():
			w.log.Info("воркер сбора информации остановлен")
			return nil
		case ip, ok := <-w.collectCh:
			if !ok {
				// Канал закрыт — завершаем работу.
				w.log.Warn("канал команд закрыт, воркер сбора информации остановлен")
				return nil
			}
			w.log.Debug("получена команда на сбор информации о коммутаторе", slog.String("ip", ip))
		}
	}
}
