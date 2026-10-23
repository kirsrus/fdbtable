// Package web содержит воркер web-сервера на базе gin.
package web

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/kirsrus/fdbtable/internal/model"
	bolt "go.etcd.io/bbolt"
)

// devicesBucket — имя bucket в базе данных для хранения информации об устройствах.
// Ключ записи — MAC-адрес устройства, значение — JSON с моделью model.Device.
const devicesBucket = "devices"

// Ошибки операций с информацией об устройствах.
var (
	errDeviceExists   = errors.New("устройство с таким MAC-адресом уже существует")
	errDeviceNotFound = errors.New("устройство не найдено")
)

// registerDevicesRoutes регистрирует маршруты страницы "Устройства".
//
// Для совместимости со старыми браузерами и отсутствия web-фреймворков
// используются максимально простые конструкции обращения:
//   - getDevices — GET, доступен из простой ссылки или формы с method="GET";
//   - setDevice, addDevice, delDevice — POST, вызываются из формы с method="POST".
func (w *Worker) registerDevicesRoutes() {
	// Страница "Устройства".
	w.engine.GET("/devices", w.getDevicesPage)

	// API списка устройств.
	w.engine.GET("/api/devices", w.getDevices)

	// API списка уникальных типов устройств, встречающихся в базе данных.
	// Используется для радиокнопок в форме добавления/редактирования устройства.
	w.engine.GET("/api/devices/types", w.getDeviceTypes)

	// API управления устройствами (вызывается формой со страницы "Устройства").
	w.engine.POST("/api/devices/add", w.addDevice)
	w.engine.POST("/api/devices/set", w.setDevice)
	w.engine.POST("/api/devices/del", w.delDevice)
}

// getDevicesPage отдаёт HTML-шаблон страницы "Устройства".
func (w *Worker) getDevicesPage(c *gin.Context) {
	c.HTML(http.StatusOK, "devices.html", gin.H{
		"title":   "fdbtable — Устройства",
		"version": w.version,
	})
}

// getDevices возвращает список устройств в формате JSON.
// Если передан параметр mac, возвращается только указанное устройство.
// Ключ записи — MAC-адрес, поэтому поиск выполняется по нему.
func (w *Worker) getDevices(c *gin.Context) {
	mac := strings.ToLower(strings.TrimSpace(getParam(c, "mac")))

	devices := make([]model.Device, 0)
	err := w.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(devicesBucket))
		if b == nil {
			// Bucket ещё не создан — устройств нет.
			return nil
		}

		// Если запрошено конкретное устройство.
		if mac != "" {
			data := b.Get([]byte(mac))
			if data == nil {
				return nil
			}
			var dev model.Device
			if err := json.Unmarshal(data, &dev); err != nil {
				return err
			}
			devices = append(devices, dev)
			return nil
		}

		// Иначе возвращаем все устройства.
		return b.ForEach(func(k, v []byte) error {
			var dev model.Device
			if err := json.Unmarshal(v, &dev); err != nil {
				return err
			}
			devices = append(devices, dev)
			return nil
		})
	})
	if err != nil {
		w.log.Error("ошибка чтения информации об устройствах", slog.Any("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": "ошибка чтения базы данных"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"ok": true, "devices": devices})
}

// getDeviceTypes возвращает список уникальных типов устройств из базы данных.
// Список формируется по значениям поля type всех записей bucket devices,
// пустые значения отбрасываются, результат сортируется по алфавиту.
// Используется для отрисовки радиокнопок в форме устройства на странице.
func (w *Worker) getDeviceTypes(c *gin.Context) {
	seen := make(map[string]struct{})
	types := make([]string, 0)

	err := w.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(devicesBucket))
		if b == nil {
			// Bucket ещё не создан — записей с типами нет.
			return nil
		}
		return b.ForEach(func(k, v []byte) error {
			var dev model.Device
			if err := json.Unmarshal(v, &dev); err != nil {
				return err
			}
			typ := strings.TrimSpace(dev.Type)
			if typ == "" {
				return nil
			}
			if _, ok := seen[typ]; ok {
				return nil
			}
			seen[typ] = struct{}{}
			types = append(types, typ)
			return nil
		})
	})
	if err != nil {
		w.log.Error("ошибка чтения типов устройств", slog.Any("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": "ошибка чтения базы данных"})
		return
	}

	// Сортируем по алфавиту для удобства выбора в форме.
	sort.Strings(types)

	c.JSON(http.StatusOK, gin.H{"ok": true, "types": types})
}

// setDevice изменяет информацию о существующем устройстве.
// Параметры передаются через форму (POST) или query-строку.
// Ключ записи — MAC-адрес устройства.
func (w *Worker) setDevice(c *gin.Context) {
	dev, err := w.parseDevice(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	err = w.db.Update(func(tx *bolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists([]byte(devicesBucket))
		if err != nil {
			return err
		}
		if b.Get([]byte(dev.MAC)) == nil {
			return errDeviceNotFound
		}
		data, err := json.Marshal(dev)
		if err != nil {
			return err
		}
		return b.Put([]byte(dev.MAC), data)
	})
	if err != nil {
		if errors.Is(err, errDeviceNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"ok": false, "error": "устройство с таким MAC-адресом не найдено"})
			return
		}
		w.log.Error("ошибка изменения информации об устройстве", slog.Any("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": "ошибка записи в базу данных"})
		return
	}

	w.log.Debug("информация об устройстве обновлена",
		slog.String("mac", dev.MAC),
		slog.String("hostname", dev.Hostname),
		slog.String("remote_addr", c.ClientIP()),
	)

	c.JSON(http.StatusOK, gin.H{"ok": true, "message": "информация об устройстве обновлена"})
}

// addDevice добавляет новое устройство в бакет devices.
// Параметры передаются через форму (POST) или query-строку.
// Ключ записи — MAC-адрес устройства, значение — JSON с моделью model.Device.
func (w *Worker) addDevice(c *gin.Context) {
	dev, err := w.parseDevice(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	err = w.db.Update(func(tx *bolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists([]byte(devicesBucket))
		if err != nil {
			return err
		}
		// MAC-адрес используется как уникальный ключ записи устройства.
		if b.Get([]byte(dev.MAC)) != nil {
			return errDeviceExists
		}
		data, err := json.Marshal(dev)
		if err != nil {
			return err
		}
		return b.Put([]byte(dev.MAC), data)
	})
	if err != nil {
		if errors.Is(err, errDeviceExists) {
			c.JSON(http.StatusConflict, gin.H{"ok": false, "error": errDeviceExists.Error()})
			return
		}
		w.log.Error("ошибка добавления устройства", slog.Any("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": "ошибка записи в базу данных"})
		return
	}

	w.log.Debug("устройство добавлено",
		slog.String("mac", dev.MAC),
		slog.String("hostname", dev.Hostname),
		slog.String("remote_addr", c.ClientIP()),
	)

	c.JSON(http.StatusOK, gin.H{"ok": true, "message": "устройство добавлено"})
}

// delDevice удаляет устройство по MAC-адресу.
func (w *Worker) delDevice(c *gin.Context) {
	mac := strings.ToLower(strings.TrimSpace(getParam(c, "mac")))
	if mac == "" {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "не указан MAC-адрес устройства"})
		return
	}

	err := w.db.Update(func(tx *bolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists([]byte(devicesBucket))
		if err != nil {
			return err
		}
		if b.Get([]byte(mac)) == nil {
			return errDeviceNotFound
		}
		return b.Delete([]byte(mac))
	})
	if err != nil {
		if errors.Is(err, errDeviceNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"ok": false, "error": "устройство с таким MAC-адресом не найдено"})
			return
		}
		w.log.Error("ошибка удаления устройства", slog.Any("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": "ошибка записи в базу данных"})
		return
	}

	w.log.Debug("устройство удалено",
		slog.String("mac", mac),
		slog.String("remote_addr", c.ClientIP()),
	)

	c.JSON(http.StatusOK, gin.H{"ok": true, "message": "устройство удалено"})
}

// parseDevice разбирает параметры устройства из формы или query-строки.
// Обязательные поля: MAC-адрес, имя устройства и тип устройства.
func (w *Worker) parseDevice(c *gin.Context) (model.Device, error) {
	dev := model.Device{
		IP:          getParam(c, "ip"),
		Hostname:    getParam(c, "hostname"),
		MAC:         getParam(c, "mac"),
		Location:    getParam(c, "location"),
		Description: getParam(c, "description"),
		Type:        getParam(c, "type"),
	}

	// Приводим MAC-адрес к нижнему регистру: ключ записи в базе данных
	// должен быть единообразным независимо от регистра ввода.
	dev.MAC = strings.ToLower(strings.TrimSpace(dev.MAC))

	// Проверка заполненности обязательных полей.
	switch {
	case dev.MAC == "":
		return dev, errors.New("не указан MAC-адрес устройства")
	case dev.Hostname == "":
		return dev, errors.New("не указано имя устройства")
	case dev.Type == "":
		return dev, errors.New("не указан тип устройства")
	}

	return dev, nil
}
