// Package web содержит воркер web-сервера на базе gin.
package web

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	bolt "go.etcd.io/bbolt"
)

// Worker — воркер web-сервера.
type Worker struct {
	port   int
	db     *bolt.DB
	log    *slog.Logger
	engine *gin.Engine
}

// New создаёт новый воркер web-сервера.
// Принимает порт, на котором будет работать сервер, базу данных и логгер.
func New(port int, db *bolt.DB, log *slog.Logger) *Worker {
	// В production-режиме отключаем debug-вывод gin.
	gin.SetMode(gin.ReleaseMode)

	engine := gin.New()
	engine.Use(gin.Logger(), gin.Recovery())

	// Загрузка HTML-шаблонов.
	engine.LoadHTMLGlob("templates/*.html")

	// Маршрут главной страницы.
	engine.GET("/", func(c *gin.Context) {
		c.HTML(http.StatusOK, "index.html", gin.H{
			"title": "fdbtable",
		})
	})

	return &Worker{
		port:   port,
		db:     db,
		log:    log,
		engine: engine,
	}
}

// Run запускает web-сервер и блокируется до его остановки.
// При отмене контекста выполняет graceful shutdown.
func (w *Worker) Run(ctx context.Context) error {
	addr := fmt.Sprintf(":%d", w.port)
	w.log.Info("запуск web-сервера", slog.String("addr", addr))

	srv := &http.Server{
		Addr:    addr,
		Handler: w.engine,
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		// Graceful shutdown при отмене контекста.
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
}
