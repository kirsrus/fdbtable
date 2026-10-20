// Package web содержит воркер web-сервера на базе gin.
package web

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"

	"github.com/gin-gonic/gin"
	bolt "go.etcd.io/bbolt"
)

// Имя bucket в базе данных для хранения информации о коммутаторах.
const swInfoBucket = "switches"

// Ошибки операций с информацией о коммутаторах.
var (
	errSwNotFound = errors.New("коммутатор не найден")
	errSwExists   = errors.New("коммутатор уже существует")
)

// SwInfo — информация о сетевом коммутаторе.
type SwInfo struct {
	IP          string `json:"ip"`
	MAC         string `json:"mac"`
	Hostname    string `json:"hostname"`
	Location    string `json:"location"`
	Description string `json:"description"`
	// CollectType — тип сбора информации о коммутаторе.
	// Хранится в текстовом виде: базовые типы (SNMP, SSH, WEB) или
	// персонализированные (например, SNMP_Cisco, SNMP_Eltex_MAS3032,
	// WEB_Rujio_808P_v433). Текстовое хранение позволяет добавлять
	// новые значения в середину списка без перенумерации.
	CollectType string `json:"collect_type"`
}

// registerSwInfoRoutes регистрирует маршруты управления информацией о коммутаторах.
//
// Для совместимости со старыми браузерами и отсутствия web-фреймворков
// используются максимально простые конструкции обращения:
//   - getSwInfo — GET, доступен из простой ссылки или формы с method="GET";
//   - setSwInfo, addSwInfo, delSwInfo, collectSwInfo — POST, вызываются из формы с method="POST".
func (w *Worker) registerSwInfoRoutes() {
	w.engine.GET("/api/swinfo", w.getSwInfo)
	w.engine.POST("/api/swinfo/set", w.setSwInfo)
	w.engine.POST("/api/swinfo/add", w.addSwInfo)
	w.engine.POST("/api/swinfo/del", w.delSwInfo)
	w.engine.POST("/api/swinfo/collect", w.collectSwInfo)
}

// getSwInfo возвращает информацию о коммутаторах в формате JSON.
// Если передан параметр ip, возвращается только указанный коммутатор.
func (w *Worker) getSwInfo(c *gin.Context) {
	ip := c.Query("ip")

	switches := make([]SwInfo, 0)
	err := w.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(swInfoBucket))
		if b == nil {
			// Bucket ещё не создан — коммутаторов нет.
			return nil
		}

		// Если запрошен конкретный коммутатор.
		if ip != "" {
			data := b.Get([]byte(ip))
			if data == nil {
				return nil
			}
			var sw SwInfo
			if err := json.Unmarshal(data, &sw); err != nil {
				return err
			}
			switches = append(switches, sw)
			return nil
		}

		// Иначе возвращаем все коммутаторы.
		return b.ForEach(func(k, v []byte) error {
			var sw SwInfo
			if err := json.Unmarshal(v, &sw); err != nil {
				return err
			}
			switches = append(switches, sw)
			return nil
		})
	})
	if err != nil {
		w.log.Error("ошибка чтения информации о коммутаторах", slog.Any("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": "ошибка чтения базы данных"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"ok": true, "switches": switches})
}

// setSwInfo изменяет информацию о существующем коммутаторе.
// Параметры передаются через форму (POST) или query-строку.
func (w *Worker) setSwInfo(c *gin.Context) {
	sw, err := w.parseSwInfo(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	err = w.db.Update(func(tx *bolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists([]byte(swInfoBucket))
		if err != nil {
			return err
		}
		if b.Get([]byte(sw.IP)) == nil {
			return errSwNotFound
		}
		data, err := json.Marshal(sw)
		if err != nil {
			return err
		}
		return b.Put([]byte(sw.IP), data)
	})
	if err != nil {
		if errors.Is(err, errSwNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"ok": false, "error": "коммутатор с таким IP не найден"})
			return
		}
		w.log.Error("ошибка изменения информации о коммутаторе", slog.Any("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": "ошибка записи в базу данных"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"ok": true, "message": "информация о коммутаторе обновлена"})
}

// addSwInfo добавляет новый коммутатор.
func (w *Worker) addSwInfo(c *gin.Context) {
	sw, err := w.parseSwInfo(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	err = w.db.Update(func(tx *bolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists([]byte(swInfoBucket))
		if err != nil {
			return err
		}
		if b.Get([]byte(sw.IP)) != nil {
			return errSwExists
		}
		data, err := json.Marshal(sw)
		if err != nil {
			return err
		}
		return b.Put([]byte(sw.IP), data)
	})
	if err != nil {
		if errors.Is(err, errSwExists) {
			c.JSON(http.StatusConflict, gin.H{"ok": false, "error": "коммутатор с таким IP уже существует"})
			return
		}
		w.log.Error("ошибка добавления коммутатора", slog.Any("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": "ошибка записи в базу данных"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"ok": true, "message": "коммутатор добавлен"})
}

// delSwInfo удаляет коммутатор по IP.
func (w *Worker) delSwInfo(c *gin.Context) {
	ip := getParam(c, "ip")
	if ip == "" {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "не указан IP-адрес коммутатора"})
		return
	}

	err := w.db.Update(func(tx *bolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists([]byte(swInfoBucket))
		if err != nil {
			return err
		}
		if b.Get([]byte(ip)) == nil {
			return errSwNotFound
		}
		return b.Delete([]byte(ip))
	})
	if err != nil {
		if errors.Is(err, errSwNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"ok": false, "error": "коммутатор с таким IP не найден"})
			return
		}
		w.log.Error("ошибка удаления коммутатора", slog.Any("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": "ошибка записи в базу данных"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"ok": true, "message": "коммутатор удалён"})
}

// collectSwInfo запускает сбор информации с коммутатора.
// Передаёт команду на сбор воркеру collector через канал и возвращает ответ.
func (w *Worker) collectSwInfo(c *gin.Context) {
	ip := getParam(c, "ip")

	w.log.Debug("вызов сбора информации о коммутаторе",
		slog.String("ip", ip),
		slog.String("remote_addr", c.ClientIP()),
	)

	// Передаём команду на сбор информации воркеру collector.
	// Неблокирующая отправка: не задерживаем HTTP-ответ, если collector занят.
	select {
	case w.collectCh <- ip:
		w.log.Info("команда на сбор информации передана воркеру collector", slog.String("ip", ip))
	default:
		w.log.Warn("воркер collector не готов принять команду на сбор информации", slog.String("ip", ip))
	}

	c.JSON(http.StatusOK, gin.H{"ok": true, "message": "сбор данных о коммутаторе запущен"})
}

// parseSwInfo разбирает параметры коммутатора из формы или query-строки.
func (w *Worker) parseSwInfo(c *gin.Context) (SwInfo, error) {
	sw := SwInfo{
		IP:          getParam(c, "ip"),
		MAC:         getParam(c, "mac"),
		Hostname:    getParam(c, "hostname"),
		Location:    getParam(c, "location"),
		Description: getParam(c, "description"),
		CollectType: getParam(c, "collect_type"),
	}

	if sw.IP == "" {
		return sw, errors.New("не указан IP-адрес коммутатора")
	}
	if net.ParseIP(sw.IP) == nil {
		return sw, errors.New("некорректный IP-адрес коммутатора")
	}

	return sw, nil
}

// getParam возвращает значение параметра из query-строки или тела формы.
// Это позволяет вызывать хэндлеры как через GET-ссылку, так и через POST-форму.
func getParam(c *gin.Context, name string) string {
	if v := c.Query(name); v != "" {
		return v
	}
	return c.PostForm(name)
}
