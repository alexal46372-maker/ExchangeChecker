# Exchange Checker

Мониторинг точек обмена 1С на Windows: служба + трей + инсталлятор.

- Политика: [`POLICY.md`](POLICY.md)
- План: [`PLAN.md`](PLAN.md)
- Поставка заказчику: [`DELIVERY.md`](DELIVERY.md)

**Статус:** фазы 0–6 ✅; фаза 7 — инсталлятор: 7a/7b ✅ (тихий режим), 7c–7e в работе. См. `docs/PHASE7.md`, PLAN §12 п. 90.

**Сборка:** Go **1.20** (`go 1.20` в `go.mod`), цель Windows amd64, `CGO_ENABLED=0`, NT 6.1+. Заказчик **не** собирает продукт — только готовый zip (см. DELIVERY.md).
