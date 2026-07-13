# image-redactor

Многопоточный редактор/конвертер изображений на Go. Два способа использования:

- **Консольная версия** (`cmd/app`) — интерактивное меню в терминале.
- **Веб-версия** (`cmd/web`) — HTTP-сервер с готовым фронтендом (встроен в бинарник через `go:embed`), даёт то же самое через браузер.

Обе версии переиспользуют один и тот же движок обработки изображений:
`internal/domain/*`, `internal/infra/codec`, `internal/infra/fs`.

## Требования

- Go 1.22+
- Внешних системных зависимостей не требуется (WebP кодируется/декодируется
  чистыми Go-библиотеками, без `libwebp`/`cwebp`).

## Установка зависимостей

```bash
go mod tidy
```

## Запуск консольной версии

```bash
go run ./cmd/app
```

Флаги: `--config configs/config.json` (по умолчанию), `--env` (настройки из
переменных окружения). Настройки, включая автоочистку папки `processed`,
см. `configs/config.json`.

## Запуск веб-версии

```bash
go run ./cmd/web
```

По умолчанию сервер поднимается на `http://localhost:8080` — откройте эту
ссылку в браузере, там сразу форма загрузки и настроек.

Флаги (все необязательные):

```bash
go run ./cmd/web \
  --addr :8080 \
  --uploads-dir ./web-data/uploads \
  --results-dir ./web-data/results \
  --results-ttl-hours 2 \
  --max-upload-mb 25
```

- `--uploads-dir` — куда временно сохраняется загруженный файл на время обработки (удаляется сразу после).
- `--results-dir` — куда сохраняются готовые файлы, доступные по ссылке для скачивания.
- `--results-ttl-hours` — через сколько часов результаты удаляются автоматически (0 — не удалять).
- `--max-upload-mb` — ограничение на размер загружаемого файла.

### API

```
POST /api/convert
  multipart/form-data:
    file               - файл изображения (jpg/png/webp)
    format             - "jpeg" | "png" | "webp"
    quality            - 1-100 (для jpeg/webp)
    png_level          - 1-10 (для png)
    crop_mode          - "none" | "manual" | "preset"
    crop_x/y/w/h       - целые числа (если crop_mode = manual)
    crop_preset        - "square" | "portrait" | "story" | "widescreen" (если crop_mode = preset)
    watermark          - "1", если нужен водяной знак
    watermark_text
    watermark_position - "top-left" | "top-right" | "bottom-left" | "bottom-right" | "center"
    watermark_opacity  - 1-100

  Ответ 200 (JSON): { "download_url", "filename", "size_before", "size_after" }
  Ответ с ошибкой:  { "error": "..." }

POST /api/convert-batch
  multipart/form-data: те же поля настроек, что и выше (общие для всей партии),
  но файл(ы) передаются полем "files" - по одному разу на каждый файл, а не "file".
  До 30 файлов за один запрос (см. maxBatchFiles в cmd/web/main.go).

  Ответ 200 (JSON):
    {
      "results": [
        { "filename": "a.jpg", "download_url": "/api/download/..", "size_before": .., "size_after": .. },
        { "filename": "b.png", "error": "сообщение об ошибке" }
      ],
      "archive_url": "/api/download/<id>.zip"  // есть, если хотя бы один файл обработан успешно
    }

  Один неудачный файл не обрывает всю партию - остальные обрабатываются как обычно,
  а он попадает в results с полем "error".

GET /api/download/{name}
  Отдаёт готовый файл (или zip-архив партии) по имени, полученному в download_url/archive_url.
```

## Исправленные баги конвертации (PNG / WebP)

- **PNG**: стандартный кодировщик `image/png` в Go поддерживает только 4
  именованных уровня сжатия (`NoCompression`, `BestSpeed`, `DefaultCompression`,
  `BestCompression`) - любое другое числовое значение он молча трактует как
  `DefaultCompression`. Раньше шкала 1-10 маппилась в промежуточные числа,
  не входящие в эти 4 константы, из-за чего 7 делений шкалы из 10 давали
  одинаковый результат ("мёртвый" слайдер). Исправлено в `mapCompressionLevel`
  (`internal/infra/codec/compressor.go`) - весь диапазон честно разбит на
  3 реальных уровня.
- **WebP**: библиотека `gowebper` трактует `Quality: 0` как отдельно
  задокументированный случай "полностью без потерь" (VP8L lossless), а любое
  значение 1-100 включает предварительное огрубление цвета. Наш домен-тип
  `compression.Quality` не допускает 0 (диапазон 1-100), поэтому раньше
  библиотека ВСЕГДА получала огрубление цвета, даже при максимальном качестве
  в интерфейсе - честный lossless получить было невозможно. Исправлено в
  `encodeWebP`: значение 100 на нашей шкале теперь отдельно превращается
  в `Quality: 0` у библиотеки.

## Сборка бинарников

```bash
go build -o bin/image-redactor-console ./cmd/app
go build -o bin/image-redactor-web ./cmd/web
```

Оба бинарника — самодостаточные (фронтенд веб-версии вшит через `go:embed`,
дополнительных файлов рядом с бинарником не требуется).

## Тесты

```bash
go test ./...
```
