# Диаграммы Saga, Outbox и DLQ

Откройте [обзор](saga-index.html) или любой HTML отдельно. Все схемы автономны;
для просмотра не требуются сервер, Node.js или установка Archify.

| Представление | HTML | Источник Archify |
|---|---|---|
| Контейнеры всей системы | [saga-containers.html](saga-containers.html) | [JSON](../sources/saga-containers.json) |
| Компоненты Order Service | [saga-components.html](saga-components.html) | [JSON](../sources/saga-components.json) |
| Sequence компенсации | [saga-sequence.html](saga-sequence.html) | [JSON](../sources/saga-sequence.json) |
| ERD orders | [saga-erd-orders.html](saga-erd-orders.html) | [JSON](../sources/saga-erd-orders.json) |
| ERD payments | [saga-erd-payments.html](saga-erd-payments.html) | [JSON](../sources/saga-erd-payments.json) |
| ERD stock | [saga-erd-stock.html](saga-erd-stock.html) | [JSON](../sources/saga-erd-stock.json) |

ERD намеренно разделена по владельцам данных. В каждой показаны все физические
столбцы, PK, FK, UK и локальные таблицы доставки. Составной FK `dead_letters`
указывает на локальный `consumer_failures`; межбазовых FK не изображено, их нет.
Один PostgreSQL контейнер на контейнерной схеме содержит три разные БД и роли.

Компонентная схема раскрывает Order. SQL repositories объединяет реальные
SagaStore, Outbox Store и DLQ Store. Sequence использует логические сервисы:
каждый включает свой consumer, БД и relay; локальные commit описаны в нотах.
Возврат показан только после успешного списания и отказа резервирования.

Схемы привязаны к commit `6a9d152` с реализацией Outbox/DLQ.
Jaeger, добавленный позднее для просмотра трасс из React, в этом снимке не показан.
`meta.repository.link_mode=local-only` сохраняет исходный SSH origin;
приватные исходники никуда не отправляются. SRC-ссылки содержат пути и диапазоны
закоммиченного кода. Дальнейшие документационные commit не изменяют эту привязку.

Генерация выполнялась установленным навыком
[tt-a1i/archify v3.2.0](https://github.com/tt-a1i/archify/tree/v3.2.0/archify).
Лицензия: [ARCHIFY-LICENSE.txt](ARCHIFY-LICENSE.txt). Русская локализация дополнена
в `docs/sources/saga-ru.json`. Проверки `validate`, `deliver`, strict artifact check
и Chrome browser-check проходят для всех шести схем; receipts сохранены в
`docs/verification/saga-*`. ERD использует standard для сохранения полной схемы;
контейнеры, компоненты и sequence — showcase.

Для повторной генерации нужны запущенные Saga/PostgreSQL, Node.js 18+,
Chrome и установленный Archify:

```bash
make saga-up
python3 scripts/generate_saga_diagrams.py --revision 6a9d152
python3 scripts/render_saga_diagrams.py
```

`ARCHIFY_CLI` позволяет указать другой путь к `bin/archify.mjs`. Генератор читает
закоммиченные исходники и PostgreSQL metadata; рендерер выполняет все проверки
последовательно, сохраняя browser evidence каждого запуска отдельно.
При изменении кода выберите соответствующий commit; для текущего HEAD —
`make saga-diagrams`. Старые Strangler-диаграммы остаются без изменений.
