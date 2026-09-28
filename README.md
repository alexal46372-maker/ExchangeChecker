# Exchange Checker

Мониторинг точек обмена 1С на Windows: служба + трей + инсталлятор.

- Политика: [`POLICY.md`](POLICY.md)
- План: [`PLAN.md`](PLAN.md)
- Поставка заказчику: [`DELIVERY.md`](DELIVERY.md)

**Статус:** фаза 0 — каркас репозитория (stubs, без бизнес-логики).

**Сборка:** Go **1.20** (`go 1.20` в `go.mod`), цель Windows amd64, `CGO_ENABLED=0`, NT 6.1+. Заказчик **не** собирает продукт — только готовый zip (см. DELIVERY.md).
