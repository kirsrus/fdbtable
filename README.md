# fdbtable

Получение FDB таблиц с сетевых коммутаторов.

## Структура проекта

```
fdbtable/
├── main.go                 # Точка входа, запуск корневой команды
├── cmd/
│   ├── root.go             # Корневая команда и глобальные флаги
│   ├── version.go          # Команда version
│   └── config.go           # Команды управления конфигурацией
└── internal/
    └── config/
        └── config.go       # Хранилище конфигурации на базе bbolt
```

## Сборка

```sh
go build ./...
```

Задание версии при сборке:

```sh
go build -ldflags "-X github.com/kirsrus/fdbtable/cmd.version=1.0.0"
```

## Использование

```sh
# Справка
fdbtable --help

# Версия
fdbtable version
```

По умолчанию база данных создаётся в файле `database.bolt`. Путь к файлу
задаётся глобальным флагом `--db`:
