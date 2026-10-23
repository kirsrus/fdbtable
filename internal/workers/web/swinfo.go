// Package web содержит воркер web-сервера на базе gin.
package web

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/kirsrus/fdbtable/internal/model"
	"github.com/kirsrus/fdbtable/internal/workers/collector"
	bolt "go.etcd.io/bbolt"
)

// Имя bucket в базе данных для хранения информации о коммутаторах.
const swInfoBucket = "switches"

// swDataBucket — имя bucket в базе данных, куда воркер collector сохраняет
// собранные данные о коммутаторах. Ключ записи — IP-адрес коммутатора,
// значение — JSON с полной моделью сбора (model.Switch).
const swDataBucket = "data"

// Ошибки операций с информацией о коммутаторах.
var (
	errSwNotFound     = errors.New("коммутатор не найден")
	errSwExists       = errors.New("коммутатор уже существует")
	errSwDataNotFound = errors.New("данные о коммутаторе не найдены")
)

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

	// Данные, собранные с коммутатора (бакет data): таблица портов и MAC.
	w.engine.GET("/api/swdata", w.getSwData)
}

// getSwInfo возвращает информацию о коммутаторах в формате JSON.
// Если передан параметр ip, возвращается только указанный коммутатор.
func (w *Worker) getSwInfo(c *gin.Context) {
	ip := c.Query("ip")

	switches := make([]model.SwInfo, 0)
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
			var sw model.SwInfo
			if err := json.Unmarshal(data, &sw); err != nil {
				return err
			}
			switches = append(switches, sw)
			return nil
		}

		// Иначе возвращаем все коммутаторы.
		return b.ForEach(func(k, v []byte) error {
			var sw model.SwInfo
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

// getSwData возвращает собранные данные о коммутаторе (модель model.Switch
// с таблицей портов и MAC-адресами) из бакета data. Ключ — IP-адрес
// коммутатора, передаётся параметром ip. Данные в бакет кладёт воркер
// collector после сбора информации с коммутатора.
//
// Если в бакете lock по этому IP-адресу есть запись (в данный момент идёт
// сбор данных), возвращается HTTP 202 Accepted с текстом выполняемой
// операции из значения записи: веб-страница может периодически опрашивать
// хэндлер и дождаться готовых данных.
func (w *Worker) getSwData(c *gin.Context) {
	ip := getParam(c, "ip")
	if ip == "" {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "не указан IP-адрес коммутатора"})
		return
	}

	// Проверяем, не выполняется ли сейчас сбор данных по этому IP-адресу.
	// Запись в бакете lock создаёт воркер collector при старте сбора и
	// удаляет после его завершения (успешного или с ошибкой).
	var lockMsg string
	err := w.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(collector.LockBucket))
		if b == nil {
			return nil
		}
		if v := b.Get([]byte(ip)); v != nil {
			lockMsg = string(v)
		}
		return nil
	})
	if err != nil {
		w.log.Error("ошибка чтения бакета lock", slog.Any("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": "ошибка чтения базы данных"})
		return
	}
	if lockMsg != "" {
		// Сбор данных ещё идёт: данные пока недоступны.
		// Текст операции отдаём из значения записи бакета lock.
		c.JSON(http.StatusAccepted, gin.H{
			"ok":      false,
			"pending": true,
			"error":   lockMsg,
		})
		return
	}

	var sw model.Switch
	err = w.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(swDataBucket))
		if b == nil {
			return errSwDataNotFound
		}
		data := b.Get([]byte(ip))
		if data == nil {
			return errSwDataNotFound
		}
		return json.Unmarshal(data, &sw)
	})
	if err != nil {
		if errors.Is(err, errSwDataNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"ok": false, "error": "данные о коммутаторе не найдены"})
			return
		}
		w.log.Error("ошибка чтения собранных данных о коммутаторе", slog.Any("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": "ошибка чтения базы данных"})
		return
	}

	// Дополняем MAC-адреса в таблице портов информацией об устройствах
	// из бакета devices: подставляем IP-адрес, тип, имя хоста,
	// местоположение и описание устройства.
	if err := w.enrichMacsWithDevices(&sw); err != nil {
		w.log.Error("ошибка обогащения MAC-адресов данными об устройствах", slog.Any("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": "ошибка чтения базы данных"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"ok": true, "switch": sw})
}

// enrichMacsWithDevices дополняет MAC-адреса всех портов коммутатора
// данными об устройствах из бакета devices (модель model.Device):
// IP-адрес, тип, имя хоста, местоположение и описание. Если устройство
// с указанным MAC-адресом не зарегистрировано, поля остаются пустыми.
//
// Ключ записи в бакете — MAC-адрес в нижнем регистре (см. addDevice),
// а StringMac в собранных данных хранится в верхнем регистре,
// поэтому перед поиском адрес приводится к нижнему регистру.
func (w *Worker) enrichMacsWithDevices(sw *model.Switch) error {
	if sw == nil {
		return nil
	}
	return w.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(devicesBucket))
		if b == nil {
			// Устройства ещё не зарегистрированы — обогащать нечем.
			return nil
		}
		for i := range sw.Ports {
			for j := range sw.Ports[i].Macs {
				mac := &sw.Ports[i].Macs[j]
				data := b.Get([]byte(strings.ToLower(mac.StringMac)))
				if data == nil {
					continue
				}
				var dev model.Device
				if err := json.Unmarshal(data, &dev); err != nil {
					return err
				}
				mac.IP = dev.IP
				mac.Type = dev.Type
				mac.Hostname = dev.Hostname
				mac.Location = dev.Location
				mac.Description = dev.Description
			}
		}
		return nil
	})
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
// Читает полную информацию о коммутаторе из базы данных (за неё отвечает
// именно web-воркер) и передаёт её воркеру collector через канал,
// после чего возвращает ответ.
func (w *Worker) collectSwInfo(c *gin.Context) {
	ip := getParam(c, "ip")
	if ip == "" {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "не указан IP-адрес коммутатора"})
		return
	}

	w.log.Debug("вызов сбора информации о коммутаторе",
		slog.String("ip", ip),
		slog.String("remote_addr", c.ClientIP()),
	)

	// Читаем полную информацию о коммутаторе из базы данных,
	// чтобы передать воркеру collector все данные о коммутаторе,
	// а не только IP-адрес.
	var sw model.SwInfo
	err := w.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(swInfoBucket))
		if b == nil {
			return errSwNotFound
		}
		data := b.Get([]byte(ip))
		if data == nil {
			return errSwNotFound
		}
		return json.Unmarshal(data, &sw)
	})
	if err != nil {
		if errors.Is(err, errSwNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"ok": false, "error": "коммутатор с таким IP не найден"})
			return
		}
		w.log.Error("ошибка чтения информации о коммутаторе из базы данных", slog.Any("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": "ошибка чтения базы данных"})
		return
	}

	// Передаём полную информацию о коммутаторе воркеру collector.
	// Неблокирующая отправка: не задерживаем HTTP-ответ, если collector занят.
	select {
	case w.collectCh <- sw:
		w.log.Debug("команда на сбор информации передана воркеру collector", slog.String("ip", sw.IP))
	default:
		w.log.Warn("воркер collector не готов принять команду на сбор информации", slog.String("ip", sw.IP))
	}

	c.JSON(http.StatusOK, gin.H{"ok": true, "message": "сбор данных о коммутаторе запущен"})
}

// parseSwInfo разбирает параметры коммутатора из формы или query-строки.
func (w *Worker) parseSwInfo(c *gin.Context) (model.SwInfo, error) {
	sw := model.SwInfo{
		IP:          getParam(c, "ip"),
		MAC:         getParam(c, "mac"),
		Hostname:    getParam(c, "hostname"),
		Location:    getParam(c, "location"),
		Description: getParam(c, "description"),
		CollectType: getParam(c, "collect_type"),
		Username:    getParam(c, "username"),
		Password:    getParam(c, "password"),
	}

	if sw.IP == "" {
		return sw, errors.New("не указан IP-адрес коммутатора")
	}
	if net.ParseIP(sw.IP) == nil {
		return sw, errors.New("некорректный IP-адрес коммутатора")
	}

	// Тип сбора должен принадлежать коллекции допустимых типов (model.CollectTypes).
	// Сравнение регистронезависимое: типы могут добавляться в разных регистрах.
	if sw.CollectType != "" && !model.CollectType(sw.CollectType).IsValid() {
		return sw, errors.New("недопустимый тип сбора информации о коммутаторе")
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
