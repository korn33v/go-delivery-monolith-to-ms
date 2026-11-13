import React, { useEffect, useState } from "react";

// No Vite proxy or direct service URLs: every API request goes to Gateway.
const API = "http://localhost:8080";
const finalStates = ["PAID", "REJECTED"];
const statusNames = {
  NEW: "Ожидает оплаты",
  PAID: "Оплачен",
  PAYMENT_UNKNOWN: "Результат отправки неизвестен",
  STOCK_UNKNOWN: "Нужна проверка резерва",
  REJECTED: "Отклонён",
};

async function request(path, options = {}) {
  const response = await fetch(API + path, options);
  const data = await response.json();
  if (!response.ok) {
    const error = new Error(data.error || `HTTP ${response.status}`);
    error.order = data.order;
    throw error;
  }
  return {
    data,
    backend: response.headers.get("X-Backend"),
    trace: response.headers.get("X-Trace-ID"),
  };
}

export default function App() {
  const [product, setProduct] = useState(null);
  const [flags, setFlags] = useState(null);
  const [stockBackend, setStockBackend] = useState("—");
  const [order, setOrder] = useState(null);
  const [trace, setTrace] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [connectionError, setConnectionError] = useState("");

  useEffect(() => {
    const controller = new AbortController();
    let timer;
    async function refresh() {
      try {
        const [stock, migration] = await Promise.all([
          request("/api/v1/stock/1", { signal: controller.signal }),
          request("/api/v1/migration", { signal: controller.signal }),
        ]);
        setProduct(stock.data);
        setStockBackend(stock.backend || "—");
        setFlags(migration.data);
        setConnectionError("");
      } catch (e) {
        if (e.name !== "AbortError") setConnectionError(e.message);
      } finally {
        if (!controller.signal.aborted) timer = setTimeout(refresh, 1000);
      }
    }
    refresh();
    return () => {
      controller.abort();
      clearTimeout(timer);
    };
  }, []);

  useEffect(() => {
    if (!order || finalStates.includes(order.status)) return;
    const controller = new AbortController();
    let timer;
    async function poll() {
      try {
        const result = await request(
          `/api/v1/orders/${encodeURIComponent(order.id)}`,
          { signal: controller.signal },
        );
        setOrder(result.data);
        if (finalStates.includes(result.data.status)) return;
      } catch (e) {
        if (e.name !== "AbortError") setError(e.message);
      }
      if (!controller.signal.aborted) timer = setTimeout(poll, 1000);
    }
    timer = setTimeout(poll, 500);
    return () => {
      controller.abort();
      clearTimeout(timer);
    };
  }, [order?.id, order?.status]);

  async function buy() {
    setBusy(true);
    setError("");
    try {
      const result = await request("/api/v1/orders", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ product_id: 1, quantity: 1 }),
      });
      setOrder(result.data);
      setTrace(result.trace || "");
    } catch (e) {
      if (e.order) setOrder(e.order);
      setError(e.message);
    } finally {
      setBusy(false);
    }
  }

  const pending = order && !finalStates.includes(order.status);
  const stage = !flags
    ? "—"
    : flags.order_microservice
      ? "04"
      : flags.async_payment
        ? "03"
        : flags.stock_microservice
          ? "02"
          : "01";

  return (
    <main>
      <header>
        <a className="brand" href="/">
          GoDelivery<span>lab</span>
        </a>
        <span className="eyebrow">АРХИТЕКТУРА И ПРОЕКТИРОВАНИЕ ПО</span>
      </header>
      <section className="intro">
        <div>
          <p className="eyebrow">STRANGLER FIG / ИНТЕРАКТИВНЫЙ СТЕНД</p>
          <h1>
            Один заказ.
            <br />
            <em>Новая архитектура.</em>
          </h1>
          <p>
            Покупка остаётся привычной. За ней монолит постепенно уступает место
            независимым сервисам.
          </p>
        </div>
        <div className="stage">
          <span>ЭТАП МИГРАЦИИ</span>
          <strong>{stage}</strong>
          <small>Флаги обновляются из .env</small>
        </div>
      </section>
      <div className="layout">
        <section className="product panel">
          <div className="illustration" aria-hidden="true">
            <svg viewBox="0 0 380 240">
              <ellipse cx="190" cy="208" rx="144" ry="13" fill="#c7d6c9" />
              <rect
                x="75"
                y="38"
                width="230"
                height="148"
                rx="9"
                fill="#24332c"
              />
              <rect
                x="84"
                y="47"
                width="212"
                height="129"
                rx="3"
                fill="#a7d3b0"
              />
              <path d="M84 154Q142 65 200 136T296 76V176H84Z" fill="#5d9879" />
              <path d="M84 166Q180 105 296 145V176H84Z" fill="#2b684f" />
              <path
                d="M75 186H305L335 204Q336 210 324 210H56Q44 210 45 204Z"
                fill="#6e8577"
              />
              <path d="M163 186H217L225 195H155Z" fill="#bacabd" />
            </svg>
            <span>GO / EVERYDAY SERIES</span>
          </div>
          <div className="product-body">
            <span className="eyebrow">ДЛЯ РАБОТЫ И НОВЫХ ИДЕЙ</span>
            <div className="product-title">
              <h2>{product?.name || "Ноутбук"}</h2>
              <strong>
                {product
                  ? (product.price_cents / 100).toLocaleString("ru-RU") + " ₽"
                  : "—"}
              </strong>
            </div>
            <p>16 ГБ памяти · SSD 512 ГБ · 14″</p>
            <div className="availability">
              <span className="dot" />
              На складе:{" "}
              <b data-testid="available">{product?.available ?? "—"}</b>
              <span className="source">{stockBackend}</span>
            </div>
            <button
              onClick={buy}
              disabled={
                busy ||
                pending ||
                !product ||
                product.available < 1 ||
                !!connectionError
              }
            >
              {busy
                ? "Оформляем…"
                : pending
                  ? "Ждём результат оплаты…"
                  : "Оформить заказ"}
              <span>↗</span>
            </button>
            <small>
              Одна покупка = один товар. Остаток обновляется каждую секунду.
            </small>
          </div>
        </section>
        <aside>
          <section className="panel journey">
            <p className="eyebrow">ПУТЬ ЗАПРОСА</p>
            <h2>Что работает сейчас</h2>
            <div className="route">
              <span>Вход в систему</span>
              <b>API Gateway · :8080</b>
            </div>
            <div className="route">
              <span>Остатки</span>
              <b>{flags?.stock_microservice ? "Stock Service" : "Монолит"}</b>
            </div>
            <div className="route">
              <span>Заказы</span>
              <b>{flags?.order_microservice ? "Order Service" : "Монолит"}</b>
            </div>
            <div className="route">
              <span>Оплата</span>
              <b>
                {flags?.order_microservice || flags?.async_payment
                  ? "Kafka → Payment Service"
                  : "Локальная функция"}
              </b>
            </div>
            <a
              href="http://localhost:8080/swagger/index.html"
              target="_blank"
              rel="noreferrer"
            >
              Открыть Swagger UI ↗
            </a>
          </section>
          <section className="panel order" aria-live="polite">
            <p className="eyebrow">ПОСЛЕДНИЙ ЗАКАЗ</p>
            {order ? (
              <>
                <div
                  className={`status ${order.status === "PAID" ? "paid" : ""}`}
                  data-testid="status"
                >
                  {statusNames[order.status] || order.status}
                </div>
                <code>{order.id}</code>
                <p>Владелец: {order.owner}</p>
                <div className="steps">
                  <span className="done">Создан</span>
                  <span className={order.status === "PAID" ? "done" : ""}>
                    Оплачен
                  </span>
                </div>
                {trace && (
                  <a
                    href={`http://localhost:16686/trace/${trace}`}
                    target="_blank"
                    rel="noreferrer"
                  >
                    Посмотреть трассу в Jaeger ↗
                  </a>
                )}
              </>
            ) : (
              <>
                <h2>
                  Всё начинается
                  <br />с одной покупки
                </h2>
                <p>Оформите заказ и наблюдайте за его статусом здесь.</p>
              </>
            )}
          </section>
        </aside>
      </div>
      {(error || connectionError) && (
        <div className="error" role="alert">
          {connectionError || error}
        </div>
      )}
      <footer>
        <span>GoDelivery / учебная миграция</span>
        <span>HTTP → Kafka → HTTP polling</span>
      </footer>
    </main>
  );
}
