// Package workers содержит воркеры приложения.
// Каждый воркер выполняет свою задачу и реализует общий интерфейс Worker.
package workers

import "context"

// Worker — интерфейс воркера приложения.
type Worker interface {
	// Run запускает воркер и блокируется до завершения его работы.
	Run(ctx context.Context) error
}
