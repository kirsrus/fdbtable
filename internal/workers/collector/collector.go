// Package collector содержит воркер сбора информации с сетевых коммутаторов.
package collector

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/kirsrus/fdbtable/internal/workers/web"
	bolt "go.etcd.io/bbolt"
	"golang.org/x/sync/errgroup"
)

// collectJobsBuffer — размер буфера очереди задач на сбор информации.
// Очередь дедуплицируется по IP-адресу коммутатора.
const collectJobsBuffer = 10

// Worker — воркер сбора информации с сетевых коммутаторов.
type Worker struct {
	db           *bolt.DB
	log          *slog.Logger
	collectCh    <-chan web.SwInfo
	workersCount int

	// collectJobs — очередь задач на сбор информации. Дедуплицируется по
	// IP-адресу коммутатора: одновременно может обрабатываться (находиться
	// в очереди или выполняться воркером) только одна задача по каждому IP.
	// Задачи в очередь кладёт диспетчер, а выполняют внутренние воркеры.
	collectJobs chan web.SwInfo

	// mu защищает jobIPs от одновременного доступа диспетчера и воркеров.
	mu     sync.Mutex
	jobIPs map[string]struct{}
}

// New создаёт новый воркер сбора информации.
// Принимает базу данных, логгер, канал команд на сбор информации
// (канал на чтение: команды приходят от web-воркера) и количество
// внутренних воркеров, выполняющих сбор данных с коммутаторов.
func New(db *bolt.DB, log *slog.Logger, collectCh <-chan web.SwInfo, workersCount int) *Worker {
	return &Worker{
		db:           db,
		log:          log,
		collectCh:    collectCh,
		workersCount: workersCount,
		collectJobs:  make(chan web.SwInfo, collectJobsBuffer),
		jobIPs:       make(map[string]struct{}),
	}
}

// Run запускает воркер и блокируется до завершения его работы.
// Диспетчер принимает команды из collectCh и кладёт их в дедуплицированную
// по IP-адресу очередь collectJobs, из которой задачи разбирают внутренние
// воркеры (по workersCount штук на каждый запуск).
func (w *Worker) Run(ctx context.Context) error {
	w.log.Debug("запуск воркера сбора информации о коммутаторах", slog.Int("workers_count", w.workersCount))

	g, gctx := errgroup.WithContext(ctx)

	// Диспетчер: принимает команды из collectCh и наполняет очередь collectJobs.
	g.Go(func() error {
		return w.dispatch(gctx)
	})

	// Внутренние воркеры: ожидают задачи из очереди collectJobs и собирают
	// информацию с коммутаторов.
	for i := 0; i < w.workersCount; i++ {
		i := i
		g.Go(func() error {
			return w.collect(gctx, i)
		})
	}

	return g.Wait()
}

// dispatch — диспетчер очереди задач.
// Получает команды на сбор информации из collectCh и добавляет их в очередь
// collectJobs, отбрасывая дубликаты по IP-адресу коммутатора.
func (w *Worker) dispatch(ctx context.Context) error {
	w.log.Debug("запуск диспетчера очереди задач")

	for {
		select {
		case <-ctx.Done():
			w.log.Debug("диспетчер очереди задач остановлен")
			return nil
		case sw, ok := <-w.collectCh:
			if !ok {
				// Канал закрыт — завершаем работу.
				w.log.Warn("канал команд закрыт, диспетчер очереди задач остановлен")
				return nil
			}

			// Проверяем, нет ли задачи по этому IP-адресу уже в очереди.
			w.mu.Lock()
			_, exists := w.jobIPs[sw.IP]
			if !exists {
				// Помечаем IP-адрес до отправки в очередь, чтобы повторные
				// команды по этому же IP не попали в очередь дубликатом.
				w.jobIPs[sw.IP] = struct{}{}
			}
			w.mu.Unlock()

			if exists {
				w.log.Warn("команда на сбор информации пропущена: коммутатор уже в очереди", slog.String("ip", sw.IP))
				continue
			}

			// Кладём задачу в очередь. Блокирующая отправка, прерываемая
			// отменой контекста.
			select {
			case w.collectJobs <- sw:
				w.log.Debug("задача добавлена в очередь на сбор информации",
					slog.String("ip", sw.IP),
					slog.String("collect_type", sw.CollectType),
				)
			case <-ctx.Done():
				return nil
			}
		}
	}
}

// collect — внутренний воркер сбора данных.
// Ожидает задачи из очереди collectJobs и собирает информацию с коммутатора.
// Пока это заглушка: логирует запуск сбора данных с коммутатора по его методу.
func (w *Worker) collect(ctx context.Context, id int) error {
	w.log.Debug("внутренний воркер сбора данных запущен", slog.Int("worker", id))

	for {
		select {
		case <-ctx.Done():
			w.log.Debug("внутренний воркер сбора данных остановлен", slog.Int("worker", id))
			return nil
		case sw, ok := <-w.collectJobs:
			if !ok {
				// Очередь задач закрыта — завершаем работу.
				w.log.Warn("очередь задач закрыта, внутренний воркер сбора данных остановлен", slog.Int("worker", id))
				return nil
			}

			// Заглушка: реальный сбор информации будет реализован позже.
			w.log.Info("запущен сбор данных с коммутатора",
				slog.Int("worker", id),
				slog.String("ip", sw.IP),
				slog.String("method", sw.CollectType),
			)

			// Эмулируем задержки.
			time.Sleep(time.Second * 15)

			// Снимаем пометку об IP-адресе: сбор данных с коммутатора
			// завершён, и по этому IP можно снова ставить задачи в очередь.
			w.mu.Lock()
			delete(w.jobIPs, sw.IP)
			w.mu.Unlock()

			w.log.Info("сбор данных с коммутатора завершон",
				slog.Int("worker", id),
				slog.String("ip", sw.IP),
				slog.String("method", sw.CollectType),
			)
		}
	}
}
