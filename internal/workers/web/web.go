// Package web содержит воркер web-сервера на базе gin.
package web

import (
	"context"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/kirsrus/fdbtable/internal/model"
	bolt "go.etcd.io/bbolt"
)

// Worker — воркер web-сервера.
type Worker struct {
	port      int
	db        *bolt.DB
	log       *slog.Logger
	engine    *gin.Engine
	version   string
	collectCh chan<- model.SwInfo
}

// New создаёт новый воркер web-сервера.
// Принимает порт, на котором будет работать сервер, базу данных, логгер,
// версию приложения (пробрасывается в шаблоны) и канал команд на сбор
// информации с коммутаторов (канал на запись: команды передаются воркеру collector).
func New(port int, db *bolt.DB, log *slog.Logger, version string, collectCh chan<- model.SwInfo) *Worker {
	// В production-режиме отключаем debug-вывод gin.
	gin.SetMode(gin.ReleaseMode)

	engine := gin.New()
	engine.Use(gin.Logger(), gin.Recovery())

	// Загрузка HTML-шаблонов из встроенного в бинарный файл набора.
	// Извлекаем под-файловую систему каталога templates, чтобы имена
	// шаблонов совпадали с именами файлов (index.html, devices.html).
	staticFS, err := fs.Sub(templatesFS, "templates")
	if err != nil {
		panic(err)
	}
	engine.SetHTMLTemplate(template.Must(template.New("").ParseFS(staticFS, "*.html")))

	// Статические файлы (styles.css), используемые шаблонами.
	engine.StaticFS("/static", http.FS(staticFS))

	// Маршрут главной страницы.
	engine.GET("/", func(c *gin.Context) {
		c.HTML(http.StatusOK, "index.html", gin.H{
			"title":   "fdbtable",
			"version": version,
			// Коллекция допустимых типов сбора информации о коммутаторе.
			// Подставляется в выпадающий список формы добавления/редактирования.
			"collectTypes": model.CollectTypes,
		})
	})

	w := &Worker{
		port:      port,
		db:        db,
		log:       log,
		engine:    engine,
		version:   version,
		collectCh: collectCh,
	}

	// Регистрация маршрутов управления информацией о коммутаторах.
	w.registerSwInfoRoutes()

	// Регистрация маршрутов страницы "Устройства".
	w.registerDevicesRoutes()

	return w
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
