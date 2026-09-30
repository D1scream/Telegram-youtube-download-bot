# Проверка текста на домашнем ПК через Tailscale

Бот работает на VPS, а модель проверки текста (`llama` + `souchastnik`) — на домашнем ПК. VPS и ПК связаны через Tailscale: это приватная сеть между твоими устройствами. Сервис проверки не открыт ни в интернет, ни в домашнюю сеть, адреса постоянные, трафик идёт напрямую и шифруется.

На ПК Tailscale работает в Docker, отдельным контейнером рядом с `souchastnik`, и ставить его в Windows не нужно. `souchastnik` использует сеть этого контейнера, поэтому доступен в сети Tailscale как отдельное устройство с именем `souchastnik`.

```
Telegram ── бот (VPS) ──Tailscale──> souchastnik:8081 (Docker на ПК) ──> llama (Docker на ПК)
```

Если ПК выключен, бот продолжает работать: фоновая проверка молча пропускает сообщения, а `/check` отвечает «Не удалось проверить текст». Когда ПК снова включится, проверка заработает сама.

## 1. Аккаунт Tailscale

Зарегистрируйся на https://login.tailscale.com. Можно войти через Google, GitHub или Microsoft. Бесплатного тарифа хватает: до 100 устройств, без лимита трафика.

## 2. Ключ для контейнера

Контейнер не может открыть браузер для входа, поэтому он подключается к сети по ключу авторизации.

1. Открой https://console.tailscale.com/admin/settings/keys.
2. Нажми **Generate auth key**. Настройки можно оставить по умолчанию, ключ одноразовый.
3. Скопируй ключ вида `tskey-auth-...`. Повторно его не покажут.

Ключ нужен только при первом запуске. Контейнер сохраняет авторизацию в папку `tailscale/` в корне проекта, и дальше ключ не используется, даже если он истёк. Папка в `.gitignore`.

## 3. Запуск на ПК

1. Положи модель в формате GGUF в `souchastnik/model/model.gguf`.
2. Положи примеры в `souchastnik/assets/examples.json`. Этот файл не хранится в git, его нужно скопировать с сервера или взять из оригинального APK.
3. Добавь ключ в `.env` в корне проекта:
   ```
   TS_AUTHKEY=tskey-auth-...
   ```
4. Запусти Tailscale, модель и сервис проверки:
   ```bash
   docker compose up -d --build tailscale llama souchastnik
   ```
5. Проверь, что контейнер вошёл в сеть, и узнай его адрес:
   ```bash
   docker exec souchastnik-tailscale tailscale status
   docker exec souchastnik-tailscale tailscale ip -4
   ```
   Адрес будет вида `100.x.x.x`. Дальше в инструкции он обозначен как `<PC_IP>`.
6. Проверь сервис изнутри контейнера:
   ```bash
   docker exec souchastnik-tailscale wget -qO- --post-data '{"text":"привет"}' http://localhost:8081/verdict
   ```
   Ожидаемый ответ: `{"code": "none"}`.

### Отключение истечения ключа устройства

По умолчанию устройство отключается от сети через 180 дней, и его нужно заново авторизовать. Чтобы этого не было:

1. Открой страницу **Machines**: https://console.tailscale.com/admin/machines.
2. В строке `souchastnik` нажми меню справа (**…**) и выбери **Disable key expiry**.

### Непрерывная работа

- **Параметры → Система → Питание → Спящий режим**: выбери «Никогда».
- **Docker Desktop → Settings → General**: включи **Start Docker Desktop when you sign in**. Контейнеры перезапускаются сами (`restart: unless-stopped`), но Docker Desktop стартует только после входа в Windows. После перезагрузки ПК нужно войти в систему, или включи автоматический вход в Windows.

Лимит `cpus: 2` у `llama` в `docker-compose.yml` нужен для VPS, где хостер ограничивает нагрузку на процессор. На ПК его можно убрать и выставить `-t` по числу ядер, тогда модель будет отвечать быстрее.

## 4. VPS (Linux)

### Установка

```bash
curl -fsSL https://tailscale.com/install.sh | sh
sudo tailscale up
```

`tailscale up` выведет ссылку. Открой её в браузере и войди в тот же аккаунт.

### Отключение истечения ключа

На https://console.tailscale.com/admin/machines выбери у VPS **… → Disable key expiry**.

### Проверка связи

```bash
tailscale status
tailscale ping <PC_IP>
```

В списке `tailscale status` должны быть VPS и `souchastnik`, а `tailscale ping` должен получить ответ `pong`.

## 5. Переключение бота на ПК

1. Проверь с VPS, что сервис на ПК доступен:
   ```bash
   curl -s http://<PC_IP>:8081/verdict -d '{"text":"привет"}'
   ```
2. Проверь то же самое из контейнера бота. Контейнер выходит в сеть через хост VPS, поэтому адрес Tailscale должен быть доступен и из него:
   ```bash
   docker exec tgbot wget -qO- --post-data '{"text":"привет"}' http://<PC_IP>:8081/verdict
   ```
3. Пропиши адрес в `~/tgbot/.env`:
   ```
   SOUCHASTNIK_URL=http://<PC_IP>:8081
   SOUCHASTNIK_TIMEOUT_SECONDS=10
   ```
4. Пересоздай контейнер бота, чтобы он перечитал `.env`:
   ```bash
   cd ~/tgbot && docker compose up -d bot
   ```
5. Отправь в чат `/check привет`. Бот должен ответить «Состав не обнаружен».

## 6. Удаление модели с VPS

Делай это только после того, как шаг 5 работает.

```bash
cd ~/tgbot
docker compose rm -sf llama souchastnik
docker rmi ghcr.io/ggml-org/llama.cpp:server tgbot-souchastnik
rm -rf souchastnik
```

Затем удали сервисы `llama` и `souchastnik` из `~/tgbot/docker-compose.yml`. На VPS освободится около 4 ГБ диска и 1.5–3 ГБ оперативной памяти.

## Если что-то не работает

| Симптом | Что проверить |
|---|---|
| `souchastnik` нет в `tailscale status` | `docker logs souchastnik-tailscale`; верный ли `TS_AUTHKEY` в `.env`. Если ключ истёк до первого входа, создай новый, удали папку `tailscale/` и перезапусти контейнер |
| `tailscale ping` не отвечает | Контейнер `souchastnik-tailscale` запущен (`docker ps`); VPS вошёл в тот же аккаунт |
| `ping` работает, а `curl` на порт 8081 нет | Контейнер `souchastnik` запущен (`docker ps`); проверка из п. 3.6 проходит |
| После перезагрузки ПК не в сети | Docker Desktop запущен (нужен вход в Windows); не истёк ли ключ устройства в админке |
| С VPS работает, а из контейнера бота нет | `docker exec tgbot ping -c1 <PC_IP>`; перезапуск Docker на VPS после установки Tailscale: `sudo systemctl restart docker` |
| Бот пишет «Не удалось проверить текст» | ПК включён и не в спящем режиме; ошибки в логе `docker logs tgbot` |
| Ответы медленные | Убери `cpus: 2` у `llama` на ПК и выставь `-t` по числу ядер |
